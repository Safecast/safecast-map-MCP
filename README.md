# Safecast MCP Server

MCP server implementations for querying Safecast radiation data.


### 🔧 Go (Experimental)

Basic MCP server skeleton with SSE transport.

- **Status:** 🚧 Early development
- **Tools:** 1/7 (ping only)
- **Transport:** SSE
- **Location:** `go/`
- **[View Documentation →](go/README.md)**

**Quick Start:**
```bash
cd go
go run cmd/mcp-server/main.go
```

---

## Features

Both implementations aim to provide:

1. ✅ **query_radiation** - Find measurements near a location
2. ✅ **get_track** - Get measurements from a specific track
3. ✅ **list_tracks** - Browse bGeigie Import tracks
4. ✅ **get_spectrum** - Get gamma spectroscopy data
5. ✅ **search_area** - Search within a bounding box
6. ✅ **radiation_info** - Educational radiation reference
7. ✅ **device_history** - Device measurement history

| Feature | Node.js | Go |
|---------|---------|-----|
| All 7 tools | ✅ | 🚧 |
| Safecast API | ✅ | 🚧 |
| Production Ready | ✅ | 🚧 |

---

## Architecture

```
Custom GPT → MCP Server → api.safecast.org
```

- No database connection required
- No authentication needed (public data)
- Read-only queries
- Results limited to ≤200 items per tool

---

## Use Cases

**For Custom GPT:**
```
"What are the radiation levels near Fukushima?"
"Show me tracks recorded in Tokyo in 2024"
"Explain radiation safety levels"
```

**For Claude Desktop:**
Add to config (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "safecast": {
      "command": "node",
      "args": ["path/to/safecast-mcp-server/nodejs/dist/index.js"]
    }
  }
}
```

---

## Contributing

See individual implementation directories for development instructions.

Node.js: [nodejs/README.md](nodejs/README.md)
Go: [go/README.md](go/README.md)

---

## License

MIT
