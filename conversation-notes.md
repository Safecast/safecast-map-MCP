# Conversation Notes: Fix MCP SSE Server 404 on POST

**Date:** 2026-02-08
**Status:** 🗄️ ARCHIVED - Historical reference only

> **Note**: This document describes the SSE server debugging process from February 2026. The implementation details may have evolved since then. For current architecture, see [README.md](README.md).

## Problem

- Opening the SSE stream at `/sse` worked and returned a session ID
- POSTing MCP messages (`initialize`, `tools/list`, `tools/call`) returned **404 page not found**

## Root Cause

In `cmd/mcp-server/main.go`, the SSE server was mounted incorrectly:

```go
http.Handle("/sse/", http.StripPrefix("/sse", sseServer))
```

The `mcp-go` library's `SSEServer.ServeHTTP` routes internally using exact path matching:
- `GET /sse` -> SSE stream handler
- `POST /message` -> message handler

With `StripPrefix("/sse", ...)`, when a client hit `GET /sse/sse`, the prefix was stripped and the handler saw `/sse` — so the SSE stream worked. But the message endpoint URL sent back to the client was `/message?sessionId=...` (no `/sse` prefix), which didn't match any route on the default mux, resulting in a 404.

## Fix Applied

Replaced the manual mux registration with `sseServer.Start()`, which sets the SSEServer as the root HTTP handler for both endpoints:

```go
// Before (broken)
sseServer := server.NewSSEServer(mcpServer)
http.Handle("/sse/", http.StripPrefix("/sse", sseServer))
http.ListenAndServe(":3333", nil)

// After (fixed)
sseServer := server.NewSSEServer(mcpServer,
    server.WithBaseURL("http://localhost:3333"),
)
sseServer.Start(":3333")
```

This lets the SSEServer handle routing itself:
- `GET /sse` -> SSE stream (returns session ID + message endpoint URL)
- `POST /message?sessionId=...` -> MCP message handler

The unused `net/http` import was also removed.

## README Update

Replaced the "What's not working" section with:
- Endpoint reference table (`GET /sse`, `POST /message`)
- Step-by-step curl testing instructions for the full MCP flow (connect, initialize, tools/list, tools/call)

## Configurable Base URL

Added `MCP_BASE_URL` environment variable support so the same binary works locally and on the VPS:

```go
baseURL := os.Getenv("MCP_BASE_URL")
if baseURL == "" {
    baseURL = "http://localhost:3333"
}
```

## VPS Deployment (vps-01.safecast.jp)

### Setup
- Binary at `/root/safecast-mcp-server/safecast-mcp`
- systemd service: `/etc/systemd/system/safecast-mcp.service`
- Environment: `MCP_BASE_URL=https://vps-01.safecast.jp/mcp`
- Apache reverse proxy in `/etc/apache2/sites-available/vps-01.safecast.jp.conf`

### Apache Proxy (added to existing 443 VirtualHost)
```apache
ProxyPass /mcp/ http://localhost:3333/mcp/
ProxyPassReverse /mcp/ http://localhost:3333/mcp/
```

### Key lesson: path consistency
When `WithBaseURL` includes a path (e.g. `/mcp`), the Go server expects that path in incoming requests. Apache must forward the full path — use `ProxyPass /mcp/ http://localhost:3333/mcp/` (not `ProxyPass /mcp/sse http://localhost:3333/sse`).

### Public endpoints
- SSE stream: `https://vps-01.safecast.jp/mcp/sse`
- POST messages: `https://vps-01.safecast.jp/mcp/message?sessionId=...`

## Files Changed

- `cmd/mcp-server/main.go` — fixed server startup, routing, and configurable base URL
- `README.md` — updated documentation with working endpoints and test instructions
