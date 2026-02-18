# MCP Tool Logging Audit Report

**Purpose:** Identify every MCP tool handler and whether it is wrapped with the AI/observability logging wrapper (`executeWithLogging` or equivalent that calls `logAISession`).

**Definitions:**
- **executeWithLogging:** Wraps a `func() (*sql.Rows, error)` call with timing and `logAISession` (structured JSON: session_id, timestamp, tool_name, generated_query, duration_ms, commit_hash, error; optional Entire HTTP export). Defined in `ai_logging.go`.
- **instrument():** Wraps a full handler with timing and `LogQueryAsync` (writes to DuckDB `mcp_query_log`). Does **not** call `logAISession`. Defined in `main.go`.
- **Tool-level wrapper (for this audit):** Presence of `executeWithLogging` (or equivalent that calls `logAISession`) somewhere in the tool’s execution path. Tools that only use `instrument()` are counted as **NOT** wrapped for AI/Entire observability.

---

## Summary

| Status | Count |
|--------|--------|
| **Wrapped** (executeWithLogging / logAISession in path) | 2 |
| **NOT wrapped** | 14 |

---

## 1. Tools WITH logging wrapper (executeWithLogging in path)

These handlers (or their callees) call `executeWithLogging`, so `logAISession` runs for at least one DB execution path.

| # | Tool name (registered) | Handler function | File | Notes |
|---|-------------------------|------------------|------|--------|
| 1 | `query_analytics` | `handleQueryAnalytics` | `go/cmd/mcp-server/tool_analytics.go` | Uses `executeWithLogging("query_analytics", analyticsQuery, fn)`. Not wrapped with `instrument()` at registration. |
| 2 | `radiation_stats` | `handleRadiationStats` | `go/cmd/mcp-server/tool_analytics.go` | Uses `executeWithLogging("radiation_stats", query, fn)`. Not wrapped with `instrument()` at registration. |

---

## 2. Tools NOT wrapped (no executeWithLogging / logAISession in path)

These tools do not call `executeWithLogging` or any equivalent that calls `logAISession`. They use Postgres (`queryRows`/`queryRow`), REST API, or static data only. They are wrapped with `instrument()` in main (except ping), which logs to DuckDB via `LogQueryAsync` only.

---

### 2.1 `ping` (health check)

| Field | Value |
|-------|--------|
| **Tool function name** | Inline handler (no named function) |
| **File path** | `go/cmd/mcp-server/main.go` |
| **Logging wrapper present** | **NO** |
| **Registration** | `mcpServer.AddTool(mcp.NewTool("ping", ...), func(ctx, req) { return mcp.NewToolResultText("pong"), nil })` |
| **Signature / return** | `func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` → returns `mcp.NewToolResultText("pong"), nil` |
| **Suggested change** | Wrap with a small helper that calls `logAISession("ping", "", durationMs, nil)` after the handler, or register as `instrument("ping", handler)` so it gets timing + LogQueryAsync. For full parity with other tools (including logAISession), extend `instrument()` to also call `logAISession(name, "", duration.Milliseconds(), err)` (see section 3). |

---

### 2.2 `query_radiation`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleQueryRadiation` |
| **File path** | `go/cmd/mcp-server/tool_query_radiation.go` |
| **Logging wrapper present** | **NO** (no `executeWithLogging`; uses `queryRows`/`queryRow` and API) |
| **Signature** | `func handleQueryRadiation(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block (DB path)** | `return queryRadiationDB(ctx, lat, lon, radiusM, limit)` or `return queryRadiationAPI(...)`. `queryRadiationDB` uses `queryRows(ctx, query, lat, lon, radiusM, limit)` and `queryRow(ctx, countQuery, ...)` — no `executeWithLogging`. |
| **Suggested change** | Do not change business logic. Add tool-level AI logging by having `instrument()` call `logAISession(name, "", duration.Milliseconds(), err)` (see section 3). Alternatively add an overload like `executeToolWithLogging("query_radiation", query, func() (*mcp.CallToolResult, error) { return queryRadiationDB(...) })` if you want to log the generated query string; that would require passing the built `query` from the handler into a wrapper. |

---

### 2.3 `search_area`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleSearchArea` |
| **File path** | `go/cmd/mcp-server/tool_search_area.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleSearchArea(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return searchAreaDB(...)` or `return searchAreaAPI(...)`. `searchAreaDB` uses `queryRows(ctx, query, ...)` and `queryRow(ctx, countQuery, ...)`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.4 `list_tracks`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleListTracks` |
| **File path** | `go/cmd/mcp-server/tool_list_tracks.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleListTracks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return listTracksAPI(...)` or `return listTracksDB(...)`. `listTracksDB` uses `queryRows(ctx, query, args...)` and `queryRow(ctx, countQuery, countArgs...)`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.5 `get_track`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleGetTrack` |
| **File path** | `go/cmd/mcp-server/tool_get_track.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleGetTrack(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return getTrackDB(...)` or `return getTrackAPI(...)`. `getTrackDB` uses `queryRows(ctx, query, args...)` and `queryRow(ctx, countQuery, ...)`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.6 `device_history`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleDeviceHistory` |
| **File path** | `go/cmd/mcp-server/tool_device_history.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleDeviceHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return deviceHistoryDB(ctx, deviceIDStr, days, limit)` or `return deviceHistoryAPI(...)`. `deviceHistoryDB` uses multiple `queryRows(ctx, markersQuery, ...)`, `queryRows(ctx, columnsQuery)`, `queryRows(ctx, realtimeQuery, ...)` — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.7 `get_spectrum`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleGetSpectrum` |
| **File path** | `go/cmd/mcp-server/tool_get_spectrum.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleGetSpectrum(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return getSpectrumDB(ctx, markerID)` or `return getSpectrumAPI(...)`. `getSpectrumDB` uses `queryRow(ctx, selectQuery, markerID)` and `queryRow(ctx, markerCheckQuery, markerID)` — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.8 `list_spectra`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleListSpectra` |
| **File path** | `go/cmd/mcp-server/tool_list_spectra.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleListSpectra(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return listSpectraDB(...)`. `listSpectraDB` uses `queryRows(ctx, baseSelect, args...)` and `queryRow(ctx, countBase, countArgs...)`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.9 `radiation_info`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleRadiationInfo` |
| **File path** | `go/cmd/mcp-server/tool_radiation_info.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleRadiationInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | Returns `jsonResult(map[string]any{"topic": normalized, "content": content})` — no DB or API call; uses static `referenceData`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.10 `db_info`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleDBInfo` |
| **File path** | `go/cmd/mcp-server/tool_db_info.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleDBInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | Returns `jsonResult(map[string]any{"status": "connected", "connection": info})`. Builds `info` via multiple `queryRow(ctx, "SELECT version()...")`, `queryRow(ctx, "SELECT current_database()...")`, etc. — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.11 `list_sensors`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleListSensors` |
| **File path** | `go/cmd/mcp-server/tool_list_sensors.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleListSensors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return listSensorsDB(...)` or error. `listSensorsDB` uses `queryRows(ctx, tablesQuery)`, `queryRows(ctx, query, args...)` — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.12 `sensor_current`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleSensorCurrent` |
| **File path** | `go/cmd/mcp-server/tool_sensor_current.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleSensorCurrent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return sensorCurrentDB(...)` or error. `sensorCurrentDB` uses `queryRows(ctx, tablesQuery)`, `queryRows(ctx, query, args...)` — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.13 `sensor_history`

| Field | Value |
|-------|--------|
| **Tool function name** | `handleSensorHistory` |
| **File path** | `go/cmd/mcp-server/tool_sensor_history.go` |
| **Logging wrapper present** | **NO** |
| **Signature** | `func handleSensorHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` |
| **Return block** | `return sensorHistoryDB(...)` or error. `sensorHistoryDB` uses `queryRows(ctx, tablesQuery)`, `queryRows(ctx, query, deviceID, startUnix, endUnix, limit)` — no `executeWithLogging`. |
| **Suggested change** | Same as 2.2: add `logAISession` at tool level via `instrument()` (section 3). |

---

### 2.14 `query_analytics` and `radiation_stats` (tool-level only)

| Field | Value |
|-------|--------|
| **Tool function names** | `handleQueryAnalytics`, `handleRadiationStats` |
| **File path** | `go/cmd/mcp-server/tool_analytics.go` |
| **Logging wrapper present** | **YES** for the DuckDB query path (they use `executeWithLogging`). **NO** at registration: they are not passed through `instrument()`, so there is no tool-level wrapper in main. |
| **Note** | If “tool level” means “one log line per tool invocation” regardless of DB, then these two have no such wrapper at the entry point; they only log when the DuckDB query runs. To get a single tool-level log line per call (e.g. with empty `generated_query` when the call fails before the query), wrap them in `instrument("query_analytics", handleQueryAnalytics)` and `instrument("radiation_stats", handleRadiationStats)` and extend `instrument()` to call `logAISession` (section 3). |

---

## 3. Recommended change: one place to cover all tools

To give every MCP tool logging, timing, and observability at the tool execution level (including `logAISession` and Entire export) without touching each handler:

**In `main.go`, extend `instrument()` to also call `logAISession`:**

```go
// instrument wraps a tool handler with logging.
func instrument(name string, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		res, err := h(ctx, req)
		duration := time.Since(start)

		// Existing DuckDB audit log
		resultCount := 0
		if res != nil {
			resultCount = len(res.Content)
		}
		args := make(map[string]any)
		if req.Params.Arguments != nil {
			if argsMap, ok := req.Params.Arguments.(map[string]any); ok {
				args = argsMap
			}
		}
		LogQueryAsync(name, args, resultCount, duration, "claude-client")

		// Add AI/Entire observability (tool-level; no SQL string for these tools)
		logAISession(name, "", duration.Milliseconds(), err)

		return res, err
	}
}
```

Then:

1. **Register analytics tools with `instrument()`** so they also get one tool-level log line (in addition to their existing `executeWithLogging` around the DuckDB query):

   ```go
   mcpServer.AddTool(queryAnalyticsToolDef, instrument("query_analytics", handleQueryAnalytics))
   mcpServer.AddTool(radiationStatsToolDef, instrument("radiation_stats", handleRadiationStats))
   ```

2. **Wrap ping** with the same pattern, e.g. register using a named handler and `instrument("ping", pingHandler)` so ping gets the same tool-level logging.

After this, every tool will have one `logAISession` call per invocation (with `generated_query` empty for non-DuckDB tools), and tools that use DuckDB will continue to have their query-level `executeWithLogging` as well.

---

## 4. File reference

| File | Handlers |
|------|----------|
| `main.go` | ping (inline) |
| `tool_analytics.go` | handleQueryAnalytics, handleRadiationStats |
| `tool_db_info.go` | handleDBInfo |
| `tool_device_history.go` | handleDeviceHistory, deviceHistoryDB, deviceHistoryAPI |
| `tool_get_spectrum.go` | handleGetSpectrum, getSpectrumDB, getSpectrumAPI |
| `tool_get_track.go` | handleGetTrack, getTrackDB, getTrackAPI |
| `tool_list_sensors.go` | handleListSensors, listSensorsDB |
| `tool_list_spectra.go` | handleListSpectra, listSpectraDB |
| `tool_list_tracks.go` | handleListTracks, listTracksDB, listTracksAPI |
| `tool_query_radiation.go` | handleQueryRadiation, queryRadiationDB, queryRadiationAPI |
| `tool_radiation_info.go` | handleRadiationInfo |
| `tool_search_area.go` | handleSearchArea, searchAreaDB, searchAreaAPI |
| `tool_sensor_current.go` | handleSensorCurrent, sensorCurrentDB |
| `tool_sensor_history.go` | handleSensorHistory, sensorHistoryDB |

---

*Audit completed. No files were modified.*
