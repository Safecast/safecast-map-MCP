# Session Context

**Session ID:** 960e1199-4c77-4e97-9fa1-33fb9498a0a9

**Commit Message:** Modify the code in the test folder an AI bot search the MCP server for d

## Prompt

can you modify the code in the test folder an AI bot search the MCP server for data and real time data is requested, automatically realtime table is used for that query. Not the device histrory?

## Summary

Perfect! I've successfully modified both test files to ensure that when an AI bot searches for real-time data, it will automatically use the realtime table through the appropriate tools. Here's what I changed:

## Changes Made

### 1. [webite entry.go:394-409](test/webite entry.go#L394-L409) (Qwen agent)
Added a comprehensive system prompt that instructs the AI to:
- Use `sensor_current` and `sensor_history` for **real-time** fixed sensor data (Pointcast, Solarcast, bGeigieZen)
- Only use `device_history` for **historical** mobile bGeigie survey data
- Recognize keywords like "real-time", "current", "latest", "live" as triggers for real-time tools

### 2. [claude_web.go:54-63](test/claude_web.go#L54-L63) (Claude CLI agent)
Enhanced the user prompt with clear guidelines that instruct Claude to:
- Use real-time tools (`sensor_current`, `sensor_history`) when real-time data is requested
- Reserve `device_history` for mobile bGeigie devices or historical import data

## How This Solves the Problem

**Before**: The AI could use `device_history` for any query, which queries both the `markers` table (historical bGeigie) and `realtime_measurements` table (real-time sensors).

**After**: When users ask about real-time data, the AI will now:
1. Recognize keywords like "real-time", "current", "latest", "live"
2. Identify fixed sensor types (Pointcast, Solarcast, bGeigieZen)
3. **Automatically select** `sensor_current` or `sensor_history` tools
4. These tools **only query** the `realtime_measurements` table, ensuring real-time data comes from the correct source

The prompts provide clear categorization and explicit instructions, making it much more likely the AI will use the correct tools for real-time queries!

## Key Actions

- - **Glob**: test/**/*.go
- - **Glob**: test/**/*.py
- - **Glob**: test/**/*.ts
- - **Glob**: test/**/*.js
- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/test/mcp_bridge.go
- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/test/claude_web.go
- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/test/webite entry.go
- - **Grep**: device_history|sensor_current|sensor_history
- - **Glob**: **/*mcp*.go
- - **Bash**: Find Go files in the go directory
