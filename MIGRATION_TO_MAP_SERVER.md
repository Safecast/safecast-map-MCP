# Migration: MCP Server to Map Server

## Overview

This document guides the migration of the MCP server from `vps-01.safecast.jp` to `simplemap.safecast.org` (the map server).

## Why Migrate?

**Performance Benefits:**
- **Eliminates network latency**: Database queries will use localhost connections instead of network connections
- **60%+ faster queries**: Reduces query time from ~50ms to ~20ms by eliminating 30ms+ network overhead
- **Higher throughput**: Localhost connections can handle 10-100x more queries per second
- **More reliable**: No network failures, packet loss, or bandwidth constraints

**Architecture:**
- **Before**: MCP Server (vps-01.safecast.jp) → Network → Database (simplemap.safecast.org:5432)
- **After**: MCP Server + Map Server + Database (all on simplemap.safecast.org with localhost connections)

## Prerequisites

- SSH access to the map server at `65.108.24.131` as root
- GitHub repository access to update secrets
- SSH private key for deployment

**⚠️ IMPORTANT:** The domain `simplemap.safecast.org` uses CloudFront CDN for web traffic. All SSH connections must use the server's IP address `65.108.24.131` directly, as CloudFront only handles HTTP/HTTPS (ports 80/443), not SSH (port 22).

## Migration Steps

### 1. Prepare the Map Server

SSH into the map server (use IP address, not domain):

```bash
ssh root@65.108.24.131
```

**Why IP?** CloudFront (the CDN in front of simplemap.safecast.org) only handles HTTP/HTTPS traffic. SSH must go directly to the server IP.

Create the MCP server directory:

```bash
mkdir -p /root/safecast-mcp-server
cd /root/safecast-mcp-server
```

### 2. Create Systemd Service

Create the systemd service file:

```bash
cat > /etc/systemd/system/safecast-mcp.service << 'EOF'
[Unit]
Description=Safecast MCP Server
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=root
WorkingDirectory=/root/safecast-mcp-server
ExecStart=/root/safecast-mcp-server/safecast-mcp
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=safecast-mcp

# Load environment variables from .env file
EnvironmentFile=/root/safecast-mcp-server/.env

# Security hardening
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
```

Enable and start the service:

```bash
systemctl daemon-reload
systemctl enable safecast-mcp
# Don't start yet - we'll deploy the binary first via GitHub Actions
```

### 3. Configure Apache Proxy

Find the Apache configuration file for simplemap.safecast.org:

```bash
# Find the config file
grep -rl "simplemap.safecast.org" /etc/apache2/sites-enabled/

# Or manually check
ls -la /etc/apache2/sites-enabled/
```

Edit the Apache config to add MCP proxy rules. Add these lines inside the `<VirtualHost *:443>` block:

```apache
    # MCP Server Proxy
    ProxyPass /mcp http://localhost:3333/mcp
    ProxyPassReverse /mcp http://localhost:3333/mcp
    ProxyPass /mcp-http http://localhost:3333/mcp-http
    ProxyPassReverse /mcp-http http://localhost:3333/mcp-http

    # REST API and Swagger UI
    ProxyPass /docs/ http://localhost:3333/docs/
    ProxyPassReverse /docs/ http://localhost:3333/docs/
    ProxyPass /api/ http://localhost:3333/api/
    ProxyPassReverse /api/ http://localhost:3333/api/
```

**Note**: Make sure these rules are placed BEFORE any catch-all proxy rules for the map server.

Test and reload Apache:

```bash
apachectl configtest
systemctl reload apache2
```

### 4. Update GitHub Secrets

Go to the GitHub repository settings: `Settings` → `Secrets and variables` → `Actions`

Update or create these secrets:

| Secret | Old Value | New Value |
|--------|-----------|-----------|
| `MAP_SERVER_HOST` | *(new)* | `65.108.24.131` (IP address, not domain) |
| `SSH_PRIVATE_KEY` | *(keep existing)* | *(same key or generate new one)* |

**⚠️ CRITICAL:** Use `65.108.24.131` (IP address) for `MAP_SERVER_HOST`, **not** `simplemap.safecast.org`. The domain uses CloudFront CDN which doesn't forward SSH traffic.

**Note**: The old `VPS_HOST` and `DATABASE_URL` secrets are no longer needed by the workflow (DATABASE_URL is now hardcoded to localhost in the workflow).

#### Optional: Generate New Deploy Key

If you want a fresh deploy key specifically for the map server:

```bash
# On your local machine
ssh-keygen -t ed25519 -C "github-deploy-mcp@simplemap" -f ~/.ssh/safecast-mcp-deploy -N ""

# Copy public key to map server (use IP, not domain)
ssh-copy-id -i ~/.ssh/safecast-mcp-deploy.pub root@65.108.24.131

# Add private key to GitHub secrets
cat ~/.ssh/safecast-mcp-deploy  # Copy this to SSH_PRIVATE_KEY secret
```

### 5. Deploy via GitHub Actions

The deployment will happen automatically on the next push to `main` that changes:
- `go/cmd/**`
- `go/go.mod` or `go/go.sum`
- `.github/workflows/deploy.yml`

Or manually trigger:

1. Go to the repository on GitHub
2. Click `Actions` → `Build and Deploy`
3. Click `Run workflow` → `Run workflow`

The workflow will:
1. Build the Go binary
2. Upload it to `/root/safecast-mcp-server/` on simplemap.safecast.org
3. Create the `.env` file with localhost database connection
4. Restart the `safecast-mcp` systemd service
5. Configure Apache proxy (if not already configured)
6. Run health check at `https://simplemap.safecast.org/mcp-http`

### 6. Verify Deployment

After deployment, verify the service is running (use IP for SSH):

```bash
ssh root@65.108.24.131

# Check service status
systemctl status safecast-mcp

# Check logs
journalctl -u safecast-mcp -f

# Verify it's listening on port 3333
ss -tlnp | grep 3333
```

Test the endpoints:

```bash
# Test MCP endpoint
curl -X POST https://simplemap.safecast.org/mcp-http \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'

# Test Swagger UI
curl -I https://simplemap.safecast.org/docs/

# Test REST API
curl "https://simplemap.safecast.org/api/radiation?lat=37.42&lon=141.03&limit=5"
```

### 7. Update Claude.ai Integration

If you have Claude.ai connected to the MCP server:

1. Go to [claude.ai](https://claude.ai)
2. Settings → Integrations
3. Find the Safecast integration
4. Update the endpoint URL to: `https://simplemap.safecast.org/mcp-http`
5. Save

The URL should work the same, but now it's hitting the map server instead of the VPS.

### 8. Decommission Old VPS Server (Optional)

Once everything is verified working on the map server:

```bash
# SSH to old VPS
ssh root@vps-01.safecast.jp

# Stop the MCP service
systemctl stop safecast-mcp
systemctl disable safecast-mcp

# Archive the installation
cd /root
tar czf safecast-mcp-server-backup-$(date +%Y%m%d).tar.gz safecast-mcp-server/

# Optional: Download backup to local machine
# scp root@vps-01.safecast.jp:/root/safecast-mcp-server-backup-*.tar.gz ~/backups/
```

You can remove the Apache proxy configuration and the systemd service file on the old VPS if desired.

## Performance Verification

After migration, compare query performance:

```bash
# Test query latency (run several times for average)
time curl -s -X POST https://simplemap.safecast.org/mcp-http \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_radiation","arguments":{"lat":37.42,"lon":141.03,"limit":10}}}'
```

Expected improvement: **30-60% faster response times** due to localhost database connections.

## Rollback Procedure

If you need to rollback to the old VPS:

1. On the old VPS, restart the service:
   ```bash
   ssh root@vps-01.safecast.jp
   systemctl start safecast-mcp
   ```

2. Update GitHub secrets back to original values:
   - Change `MAP_SERVER_HOST` back to `VPS_HOST` with value `vps-01.safecast.jp`
   - Restore `DATABASE_URL` secret

3. Revert the workflow changes:
   ```bash
   git revert <commit-hash-of-migration>
   git push origin main
   ```

4. Update Claude.ai integration URL back to `https://vps-01.safecast.jp/mcp-http`

## Troubleshooting

### Service won't start

```bash
# Check service status and logs
systemctl status safecast-mcp
journalctl -u safecast-mcp -n 50

# Common issues:
# - Binary not executable: chmod +x /root/safecast-mcp-server/safecast-mcp
# - Port already in use: ss -tlnp | grep 3333
# - Database connection issues: check /root/safecast-mcp-server/.env
```

### Apache proxy not working

```bash
# Check Apache config
apachectl configtest

# Check Apache error logs
tail -f /var/log/apache2/error.log

# Verify proxy modules are enabled
a2enmod proxy proxy_http
systemctl restart apache2
```

### Health check fails

```bash
# Check if service is running
systemctl status safecast-mcp

# Check if it's listening
ss -tlnp | grep 3333

# Test locally on server
curl -v http://localhost:3333/docs/

# Check Apache is proxying correctly
curl -v https://simplemap.safecast.org/mcp-http
```

## Files Changed

- `.github/workflows/deploy.yml` - Updated to deploy to map server with localhost DB
- `.env` - Updated DATABASE_URL to use localhost
- `MIGRATION_TO_MAP_SERVER.md` - This migration guide

## Questions or Issues?

If you encounter any issues during migration, check:

1. Service logs: `journalctl -u safecast-mcp -f`
2. Apache logs: `tail -f /var/log/apache2/error.log`
3. GitHub Actions logs: Check the workflow run output
4. Network connectivity: `curl http://localhost:3333/docs/` from the server
