# Session Context

**Session ID:** b6847497-f5f3-4b52-a8dd-39eafaa75c69

**Commit Message:** Also do the same for the chnages we made for use of the real time sensor

## Prompt

can you also do the same for the chnages we made for use of the real time sensors tables and other chnages to the instructions for AI bots?

## Summary

Perfect! I've updated all three MCP tool descriptions with both the CPM/CPS clarification **and** the real-time vs historical data guidance:

## Updated Files:

1. **[tool_sensor_current.go:11](go/cmd/mcp-server/tool_sensor_current.go#L11)**
   - Now specifies: "REAL-TIME fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.)"
   - Clarifies: "NOT for mobile bGeigie devices"
   - States: "CPM means 'counts per minute' (NOT counts per second)"

2. **[tool_sensor_history.go:12](go/cmd/mcp-server/tool_sensor_history.go#L12)**
   - Now specifies: "REAL-TIME fixed sensors" for time-series data
   - Clarifies: "NOT for mobile bGeigie devices - use device_history"
   - States: "CPM means 'counts per minute' (NOT counts per second)"

3. **[tool_device_history.go:11](go/cmd/mcp-server/tool_device_history.go#L11)**
   - Now specifies: "MOBILE bGeigie survey devices or historical import data"
   - Clarifies: "Use ONLY for mobile devices, NOT for real-time fixed sensors"
   - States: "CPM (counts per minute, NOT counts per second)"

These changes will affect **all clients** using the MCP server (Claude CLI, the web interfaces, etc.) and should prevent both the CPS/CPM confusion and the incorrect tool selection for real-time sensors.

To deploy these changes, you'll need to rebuild and redeploy the Go binary.

## Key Actions

- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_sensor_current.go
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_sensor_history.go
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_device_history.go
- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_sensor_history.go
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_sensor_history.go
- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_device_history.go
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/go/cmd/mcp-server/tool_device_history.go
