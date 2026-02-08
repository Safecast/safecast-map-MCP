# Safecast MCP Server

An MCP (Model Context Protocol) server that exposes [Safecast](https://safecast.org) radiation monitoring data to AI assistants like Claude. The server provides 8 tools for querying radiation measurements, browsing sensor tracks, and accessing educational reference data.

## Features

- **8 tools** for querying Safecast radiation data
- **Dual transport**: SSE and Streamable HTTP (works with Claude.ai)
- **PostgreSQL + PostGIS** for fast spatial queries (with REST API fallback)
- **Read-only** access to public Safecast data

## Tools

| Tool | Description |
|------|-------------|
| `ping` | Health check |
| `query_radiation` | Find measurements near a lat/lon coordinate |
| `search_area` | Search within a geographic bounding box |
| `list_tracks` | Browse bGeigie Import tracks by year/month |
| `get_track` | Get measurements from a specific track |
| `device_history` | Historical data from a monitoring device |
| `get_spectrum` | Gamma spectroscopy data for a measurement |
| `radiation_info` | Educational reference (units, safety levels, detectors, etc.) |

## Quick Start

```bash
cd go
go build -o safecast-mcp ./cmd/mcp-server/
./safecast-mcp
```

The server listens on port 3333 by default.

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MCP_BASE_URL` | No | Base URL advertised by the SSE transport so clients know where to POST messages back (default: `http://localhost:3333`). Must **not** include `/mcp` — the server appends that automatically. |
| `DATABASE_URL` | No | PostgreSQL connection string. If not set, uses the Safecast REST API. |

### Endpoints

- **SSE**: `/mcp/sse` (GET) and `/mcp/message` (POST)
- **Streamable HTTP**: `/mcp-http` (POST)

## Connecting to Claude.ai

1. Open [claude.ai](https://claude.ai) in your browser
2. Go to **Settings** (bottom-left) > **Integrations**
3. Click **Add more** > **Add custom integration**
4. Enter a name (e.g. "Safecast") and paste the Streamable HTTP endpoint URL:
   ```
   https://vps-01.safecast.jp/mcp-http
   ```
5. Click **Save** — the Safecast tools will now be available in your conversations

## Architecture

```
Claude / AI client
    |
    v
MCP Server (Go, mcp-go)
    |
    +---> PostgreSQL + PostGIS (primary, if DATABASE_URL set)
    |
    +---> api.safecast.org REST API (fallback)
```

The server uses [`mcp-go`](https://github.com/mark3labs/mcp-go) for MCP protocol support. All tools attempt a direct database query first and fall back to the Safecast REST API if no database is configured or the query fails.

## Project Structure

```
go/cmd/mcp-server/
  main.go              # Server setup, tool registration, dual transport
  api_client.go        # Safecast REST API client
  db_client.go         # PostgreSQL connection pool (pgx)
  reference_data.go    # Static radiation reference data
  tool_query_radiation.go
  tool_search_area.go
  tool_list_tracks.go
  tool_get_track.go
  tool_device_history.go
  tool_get_spectrum.go
  tool_radiation_info.go
```

## Development

```bash
cd go
go run ./cmd/mcp-server/
```

Cross-compile for Linux deployment:

```bash
cd go
GOOS=linux GOARCH=amd64 go build -o safecast-mcp ./cmd/mcp-server/
```

## Contributing

Contributions welcome. If changing a tool's interface, please open an issue first. Fork, branch, PR.

## License

MIT
