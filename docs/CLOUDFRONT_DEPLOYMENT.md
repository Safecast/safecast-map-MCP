# CloudFront Deployment Guide for MCP Server

## Overview

The MCP server is deployed on `simplemap.safecast.org` which uses AWS CloudFront as a CDN. Understanding the traffic flow is critical for proper deployment.

## Infrastructure

**Server IP:** 65.108.24.131
**Domain:** simplemap.safecast.org
**CDN:** AWS CloudFront

## Traffic Flow

```
┌─────────────────────────────────────────────────────────┐
│              HTTPS Traffic (Ports 80/443)               │
│                    ✅ Works via Domain                   │
└─────────────────────────────────────────────────────────┘
  User/AI Client
       ↓
  simplemap.safecast.org (DNS → CloudFront)
       ↓
  CloudFront Distribution
       ↓
  Apache Reverse Proxy (on 65.108.24.131)
       ↓
  MCP Server (localhost:3333)
       ↓
  PostgreSQL Database (localhost:5432)


┌─────────────────────────────────────────────────────────┐
│               SSH/Rsync Traffic (Port 22)               │
│             ❌ MUST Use IP Address Directly              │
└─────────────────────────────────────────────────────────┘
  Developer Machine
       ↓
  65.108.24.131 (Direct IP - bypasses CloudFront)
       ↓
  SSH Server on 65.108.24.131
```

## Critical: SSH and CloudFront

**CloudFront only handles HTTP/HTTPS traffic** (ports 80/443). It does NOT forward:
- SSH (port 22)
- Database connections (port 5432)
- Any other TCP traffic

### The Problem

If you try to SSH using the domain name:
```bash
ssh root@simplemap.safecast.org  # ❌ Will FAIL
```

What happens:
1. DNS resolves `simplemap.safecast.org` to CloudFront's IP addresses
2. Your SSH client tries to connect to CloudFront's servers on port 22
3. CloudFront doesn't accept SSH connections
4. Connection fails or times out

### The Solution

Always use the IP address for SSH/rsync:
```bash
ssh root@65.108.24.131  # ✅ Correct
scp file.txt root@65.108.24.131:/path/  # ✅ Correct
rsync -avz ./local/ root@65.108.24.131:/remote/  # ✅ Correct
```

## Code Compatibility

**Good news:** The MCP server code requires **NO modifications** to work with CloudFront.

### Why It Works

1. **HTTPS URLs use domain name** - CloudFront forwards these correctly
   - `MCP_BASE_URL=https://simplemap.safecast.org/mcp` ✅
   - API fallback: `https://simplemap.safecast.org/api/radiation` ✅
   - Swagger UI: `https://simplemap.safecast.org/docs/` ✅

2. **Database uses localhost** - Not affected by CloudFront
   - `DATABASE_URL=postgres://...@localhost:5432/safecast` ✅

3. **Server listens on localhost** - Apache proxies requests
   - MCP listens on `localhost:3333` ✅
   - Apache forwards from CloudFront to localhost ✅

## GitHub Actions Deployment

The workflow has been updated to separate SSH host from HTTPS URLs.

### Required Secrets

| Secret | Value | Purpose |
|--------|-------|---------|
| `SSH_PRIVATE_KEY` | Your SSH private key | Authentication for deployment |
| `MAP_SERVER_HOST` | **`65.108.24.131`** | Server IP for SSH/rsync |

**⚠️ CRITICAL:** `MAP_SERVER_HOST` must be the **IP address** (65.108.24.131), not the domain name.

### How the Workflow Works

```yaml
env:
  MAP_SERVER_HOST: ${{ secrets.MAP_SERVER_HOST }}  # IP: 65.108.24.131
  MAP_SERVER_DOMAIN: simplemap.safecast.org         # Hardcoded domain

# SSH uses IP address
SSH="ssh -i ~/.ssh/deploy_key root@$MAP_SERVER_HOST"  # Uses 65.108.24.131

# HTTPS URL uses domain name
ENV_FILE="MCP_BASE_URL=https://$MAP_SERVER_DOMAIN/mcp"  # Uses simplemap.safecast.org
```

This ensures:
- ✅ SSH/rsync connects to the IP (bypasses CloudFront)
- ✅ HTTPS URLs use the domain (goes through CloudFront)
- ✅ Users access the MCP via CloudFront (fast, global CDN)
- ✅ Deployment works correctly

## Manual Deployment

When deploying manually, always use the IP address:

```bash
cd go
GOOS=linux GOARCH=amd64 go build -o safecast-mcp ./cmd/mcp-server/

# ✅ Correct - Use IP address
scp safecast-mcp root@65.108.24.131:/root/safecast-mcp-server/
ssh root@65.108.24.131 "systemctl restart safecast-mcp"

# ❌ Wrong - Domain won't work for SSH
scp safecast-mcp root@simplemap.safecast.org:/root/safecast-mcp-server/  # FAILS!
```

## Verifying Deployment

After deployment, verify both SSH access and HTTPS endpoints:

### 1. SSH Access (Direct IP)
```bash
# ✅ This should work
ssh root@65.108.24.131 "systemctl status safecast-mcp"

# ❌ This will fail
ssh root@simplemap.safecast.org "systemctl status safecast-mcp"
```

### 2. HTTPS Endpoints (Through CloudFront)
```bash
# ✅ All of these should work via domain
curl https://simplemap.safecast.org/docs/
curl https://simplemap.safecast.org/api/radiation?lat=37.42&lon=141.03&limit=5

# Test MCP endpoint
curl -X POST https://simplemap.safecast.org/mcp-http \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
```

### 3. Check CloudFront Headers
```bash
curl -I https://simplemap.safecast.org/docs/
# Look for CloudFront headers:
# x-cache: Hit from cloudfront
# x-amz-cf-pop: NRT57-P1
# via: 1.1 xxxxx.cloudfront.net (CloudFront)
```

## Troubleshooting

### Problem: SSH Connection Refused

**Symptom:**
```
ssh: connect to host simplemap.safecast.org port 22: Connection refused
```

**Cause:** Trying to SSH to the domain name (CloudFront IP) instead of the server IP.

**Solution:** Use IP address:
```bash
ssh root@65.108.24.131
```

### Problem: GitHub Actions Deploy Fails with SSH Error

**Symptom:**
```
Permission denied (publickey)
Host key verification failed
```

**Cause:** `MAP_SERVER_HOST` secret is set to domain name instead of IP.

**Solution:** Update GitHub secret:
1. Go to repository Settings → Secrets and variables → Actions
2. Update `MAP_SERVER_HOST` to `65.108.24.131`
3. Re-run the workflow

### Problem: MCP Endpoints Return 502 Bad Gateway

**Symptom:**
```bash
curl https://simplemap.safecast.org/mcp-http
# Returns: 502 Bad Gateway
```

**Cause:** MCP server not running or Apache proxy not configured.

**Check:**
```bash
# SSH to server (use IP)
ssh root@65.108.24.131

# Check if MCP is running
systemctl status safecast-mcp

# Check if it's listening
ss -tlnp | grep 3333

# Check Apache proxy config
grep -r "ProxyPass.*mcp" /etc/apache2/
```

### Problem: Database Connection Errors

**Symptom:** MCP server logs show database connection errors.

**Cause:** `DATABASE_URL` in `/root/safecast-mcp-server/.env` is incorrect.

**Check:**
```bash
ssh root@65.108.24.131
cat /root/safecast-mcp-server/.env

# Should show:
# DATABASE_URL=postgres://mcp_readonly:PASSWORD@localhost:5432/safecast
# MCP_BASE_URL=https://simplemap.safecast.org/mcp
# DUCKDB_PATH=/root/safecast-mcp-server/analytics.duckdb
```

**Important:** Database connection must use `localhost`, not an external IP or domain.

## Benefits of CloudFront Setup

### For End Users
- **Faster response times** - Content cached at edge locations worldwide
- **Lower latency** - Tokyo, Europe, US users all get fast access
- **Better reliability** - DDoS protection, automatic failover
- **HTTPS security** - Free SSL certificate, modern TLS

### For Deployment
- **Direct server access** - SSH still works via IP address
- **Optimal database performance** - MCP connects to localhost (no network overhead)
- **Transparent operation** - MCP server code unchanged
- **Cache control** - API responses not cached (dynamic data)

## Environment Variables

The MCP server uses these environment variables (set in `.env` file):

```bash
# Base URL for SSE transport - uses domain (goes through CloudFront)
MCP_BASE_URL=https://simplemap.safecast.org/mcp

# Database connection - uses localhost (direct connection)
DATABASE_URL=postgres://mcp_readonly:PASSWORD@localhost:5432/safecast

# DuckDB analytics - local file path
DUCKDB_PATH=/root/safecast-mcp-server/analytics.duckdb
```

These are automatically set by the GitHub Actions workflow.

## Apache Proxy Configuration

The Apache configuration on the server proxies CloudFront requests to the MCP server:

```apache
<VirtualHost *:443>
    ServerName simplemap.safecast.org

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

    # ... other configuration ...
</VirtualHost>
```

## Summary

### ✅ Correct Usage

```bash
# SSH/Deployment - Use IP
ssh root@65.108.24.131
scp file root@65.108.24.131:/path/
rsync -avz ./dir/ root@65.108.24.131:/path/

# HTTPS/API Access - Use domain (goes through CloudFront)
curl https://simplemap.safecast.org/mcp-http
curl https://simplemap.safecast.org/docs/
curl https://simplemap.safecast.org/api/radiation?lat=37.42&lon=141.03
```

### ❌ Incorrect Usage

```bash
# SSH with domain - WILL FAIL
ssh root@simplemap.safecast.org  # CloudFront can't handle SSH!

# Hardcoding IP in HTTPS URLs - BAD PRACTICE
curl http://65.108.24.131:3333/mcp-http  # Bypasses CloudFront, no caching
```

## Related Documentation

- [README.md](../README.md) - Main documentation
- [MIGRATION_TO_MAP_SERVER.md](../MIGRATION_TO_MAP_SERVER.md) - Server migration guide
- [Main Map Server CloudFront Guide](../../safecast-new-map/docs/cloudfront-setup.md) - CloudFront configuration

## Questions?

If you encounter issues:
1. Verify `MAP_SERVER_HOST` secret is set to IP: `65.108.24.131`
2. Check SSH access works: `ssh root@65.108.24.131`
3. Check HTTPS endpoints work: `curl https://simplemap.safecast.org/docs/`
4. Review GitHub Actions logs for deployment errors
5. Check server logs: `ssh root@65.108.24.131 "journalctl -u safecast-mcp -f"`
