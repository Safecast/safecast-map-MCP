# Safecast MCP Server connecting to simplemap.safecast.org V0.9

An MCP (Model Context Protocol) server that exposes [Safecast](https://safecast.org) radiation monitoring data to AI assistants like Claude. The server provides 15 tools for querying both real-time sensor readings and historical radiation measurements, browsing sensor tracks, spectroscopy data, analytics, and educational reference data.

## Features

- **15 tools** for querying Safecast radiation data
- **Real-time and historical data access**: Query both real-time sensor readings and historical measurements
- **Dual transport**: SSE and Streamable HTTP (works with Claude.ai)
- **PostgreSQL + PostGIS** for fast spatial queries (with REST API fallback)
- **DuckDB analytics** for usage statistics and aggregate queries
- **Structured runtime logging** for monitoring tool usage and performance
- **Read-only** access to public Safecast data

## Tools Overview

| Tool | Data Type | Description |
|------|-----------|-------------|
| `query_radiation` | Historical | Find measurements near a lat/lon coordinate |
| `search_area` | Historical | Search within a geographic bounding box |
| `list_tracks` | Historical | Browse bGeigie Import tracks by year/month |
| `get_track` | Historical | Get measurements from a specific track |
| `device_history` | Mixed | Historical data from a monitoring device (supports both bGeigie and real-time sensors) |
| `list_sensors` | Real-time | Discover active fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.) by location or type |
| `sensor_current` | Real-time | Get the latest reading(s) from a specific sensor or from all sensors in a geographic area |
| `sensor_history` | Real-time | Pull time-series data from a fixed sensor over a date range |
| `list_spectra` | Historical | Browse and search gamma spectroscopy records |
| `get_spectrum` | Historical | Get full spectroscopy channel data for a measurement |
| `radiation_info` | Reference | Educational reference (units, safety levels, detectors, isotopes) |
| `radiation_stats` | Aggregate | Aggregate radiation statistics by year/month |
| `query_analytics` | Analytics | Server usage statistics (call counts, durations) |
| `db_info` | Diagnostic | Database connection and status (diagnostic) |
| `ping` | Diagnostic | Health check |

## Real-time Data Access

The Safecast MCP server provides access to real-time radiation data from fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.) through dedicated tools. These tools query the `realtime_measurements` table in the PostgreSQL database to retrieve the most current readings from active sensors.

### Real-time Tools
- `list_sensors`: Discover active fixed sensors by location or type
- `sensor_current`: Get the latest reading(s) from specific sensors or geographic areas
- `sensor_history`: Pull time-series data from fixed sensors over date ranges
- `device_history`: Access both historical bGeigie data and real-time sensor data for a specific device

> **Note**: Real-time data tools require a database connection to access the `realtime_measurements` table. These tools will fall back to the Safecast REST API if no database is configured.

## Tool Reference

### query_radiation

Find radiation measurements near a geographic location. Returns measurements within a specified radius, sorted by most recent.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `lat` | number | Yes | | Latitude (-90 to 90) |
| `lon` | number | Yes | | Longitude (-180 to 180) |
| `radius_m` | number | No | 1500 | Search radius in meters (25 to 50,000) |
| `limit` | number | No | 25 | Max results (1 to 10,000) |

**Example**: Find measurements within 5km of Fukushima Daiichi:
```json
{"name": "query_radiation", "arguments": {"lat": 37.42, "lon": 141.03, "radius_m": 5000}}
```

Each result includes: `id`, `value` (dose rate in uSv/h), `captured_at`, `location` (lat/lon), `device_id`, `detector`, `track_id`, `has_spectrum`, and `distance_m`.

---

### search_area

Find radiation measurements within a geographic bounding box.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `min_lat` | number | Yes | | Southern boundary latitude |
| `max_lat` | number | Yes | | Northern boundary latitude |
| `min_lon` | number | Yes | | Western boundary longitude |
| `max_lon` | number | Yes | | Eastern boundary longitude |
| `limit` | number | No | 100 | Max results (1 to 10,000) |

**Example**: Search the Tokyo metropolitan area:
```json
{"name": "search_area", "arguments": {"min_lat": 35.5, "max_lat": 35.8, "min_lon": 139.5, "max_lon": 139.9}}
```

---

### list_tracks

Browse bGeigie Import tracks (bulk radiation measurement drives/journeys). Each track represents a set of measurements collected during a single bGeigie session.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `year` | number | No | | Filter by year (2000-2100) |
| `month` | number | No | | Filter by month (1-12, requires `year`) |
| `limit` | number | No | 50 | Max results (1 to 50,000) |

**Example**: Browse tracks from January 2024:
```json
{"name": "list_tracks", "arguments": {"year": 2024, "month": 1}}
```

Each result includes: `track_id`, `name`, `description`, `city`, `measurement_count`, `created_at`, and status info.

---

### get_track

Retrieve all radiation measurements from a specific track/journey. Use `list_tracks` to find track IDs first.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `track_id` | string | Yes | | Track identifier |
| `from` | number | No | | Start marker ID for filtering |
| `to` | number | No | | End marker ID for filtering |
| `limit` | number | No | 200 | Max results (1 to 10,000) |

**Example**: Get measurements from a specific track:
```json
{"name": "get_track", "arguments": {"track_id": "8eh5m1"}}
```

---

### device_history

Get historical radiation measurements from a specific monitoring device over a time period. This tool now supports both bGeigie import data and real-time sensor data.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `device_id` | string | Yes | | Device identifier |
| `days` | number | No | 30 | Days of history (1 to 365) |
| `limit` | number | No | 200 | Max results (1 to 10,000) |

**Example**: Get 90 days of history from a device:
```json
{"name": "device_history", "arguments": {"device_id": "12345", "days": 90}}
```

> **Note**: This tool queries both the `markers` table (for bGeigie imports) and the `realtime_measurements` table (for fixed sensors) to provide a comprehensive history from the specified device.

---

### list_sensors

Discover active fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.) by location or type, returning device IDs, locations, status, and last reading timestamp.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `type` | string | No | | Filter by sensor type (e.g., 'Pointcast', 'Solarcast', 'bGeigieZen') |
| `min_lat` | number | No | -90 | Southern boundary for geographic filter |
| `max_lat` | number | No | 90 | Northern boundary for geographic filter |
| `min_lon` | number | No | -180 | Western boundary for geographic filter |
| `max_lon` | number | No | 180 | Eastern boundary for geographic filter |
| `limit` | number | No | 50 | Max results (1 to 1000) |

**Example**: Find all Pointcast sensors in Japan:
```json
{"name": "list_sensors", "arguments": {"type": "Pointcast", "min_lat": 30, "max_lat": 46, "min_lon": 129, "max_lon": 146}}
```

> **Note**: Requires database connection to access `realtime_measurements` table.

---

### sensor_current

Get the latest reading(s) from a specific sensor or from all sensors in a geographic area.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `device_id` | string | No | | Specific device ID to get latest reading from |
| `min_lat` | number | No | -90 | Southern boundary for geographic filter |
| `max_lat` | number | No | 90 | Northern boundary for geographic filter |
| `min_lon` | number | No | -180 | Western boundary for geographic filter |
| `max_lon` | number | No | 180 | Eastern boundary for geographic filter |
| `limit` | number | No | 25 | Max results (1 to 1000) |

**Example**: Get latest reading from a specific sensor:
```json
{"name": "sensor_current", "arguments": {"device_id": "sensor-123"}}
```

**Example**: Get latest readings from all sensors in Tokyo:
```json
{"name": "sensor_current", "arguments": {"min_lat": 35.5, "max_lat": 35.8, "min_lon": 139.5, "max_lon": 139.9, "limit": 50}}
```

> **Note**: Requires database connection to access `realtime_measurements` table.

---

### sensor_history

Pull time-series data from a fixed sensor over a date range.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `device_id` | string | Yes | | Device identifier to get historical data from |
| `start_date` | string | Yes | | Start date in YYYY-MM-DD format |
| `end_date` | string | No | Today | End date in YYYY-MM-DD format |
| `limit` | number | No | 200 | Max results (1 to 10,000) |

**Example**: Get 30 days of history from a sensor:
```json
{"name": "sensor_history", "arguments": {"device_id": "sensor-123", "start_date": "2024-01-01", "end_date": "2024-01-31"}}
```

> **Note**: Requires database connection to access `realtime_measurements` table.

---

### list_spectra

Browse and search gamma spectroscopy records. Returns metadata (filename, device, energy range, location) **without** the full channel data. Use `get_spectrum` with a `marker_id` from the results to fetch full channel data.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `min_lat` | number | No | | Southern boundary (requires all 4 bbox params) |
| `max_lat` | number | No | | Northern boundary (requires all 4 bbox params) |
| `min_lon` | number | No | | Western boundary (requires all 4 bbox params) |
| `max_lon` | number | No | | Eastern boundary (requires all 4 bbox params) |
| `source_format` | string | No | | Filter by file format (e.g., `"spe"`, `"csv"`) |
| `device_model` | string | No | | Filter by detector name (partial match) |
| `track_id` | string | No | | Filter by track identifier (e.g., `"8eh5m1"`, `"8ZnI7f"`) |
| `limit` | number | No | 50 | Max results (1 to 500) |

**Example**: Find all SPE spectrum files:
```json
{"name": "list_spectra", "arguments": {"source_format": "spe"}}
```

**Example**: Browse all spectra (no filters):
```json
{"name": "list_spectra", "arguments": {}}
```

**Example**: Get all spectrum files for a specific track:
```json
{"name": "list_spectra", "arguments": {"track_id": "8eh5m1"}}
```

**Example**: Get all spectrum files regardless of track (no filters):
```json
{"name": "list_spectra", "arguments": {"limit": 100}}
```

Each result includes: `spectrum_id`, `marker_id`, `filename`, `source_format`, `device_model`, `channel_count`, `energy_range`, `live_time_sec`, `calibration`, `created_at`, and nested `marker` with location and track_id.

> **Note**: Requires database connection. No REST API fallback.

---

### get_spectrum

Get full gamma spectroscopy channel data for a specific measurement point. Returns the complete spectrum including all channel counts.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `marker_id` | number | Yes | | Marker/measurement identifier (get from `list_spectra`) |

**Example**:
```json
{"name": "get_spectrum", "arguments": {"marker_id": 4902886}}
```

Returns: `channels` (array of counts), `channel_count`, `energy_min_kev`, `energy_max_kev`, `live_time_sec`, `real_time_sec`, `device_model`, `calibration`, `source_format`, `filename`, plus marker location and dose rate.

---

### radiation_info

Get educational reference information about radiation. Returns static content.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `topic` | string | Yes | | One of: `units`, `dose_rates`, `safety_levels`, `detectors`, `background_levels`, `isotopes` |

**Topics**:
- `units` -- Explanation of uSv/h, CPM, Bq, Sv
- `dose_rates` -- Typical dose rate ranges and what they mean
- `safety_levels` -- International safety standards and thresholds
- `detectors` -- Types of radiation detectors and how they work
- `background_levels` -- Natural background radiation by region
- `isotopes` -- Common radioactive isotopes and their properties

---

### radiation_stats

Get aggregate radiation statistics from the Safecast database grouped by time interval. Powered by DuckDB + PostgreSQL.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `interval` | string | No | `"year"` | Aggregation: `"year"`, `"month"`, or `"overall"` |

**Example**: Get yearly statistics:
```json
{"name": "radiation_stats", "arguments": {"interval": "year"}}
```

---

### query_analytics

Get usage statistics for all MCP tools including call counts, average duration, and max duration. Powered by DuckDB local logs. No parameters required.

### Structured Runtime Logging

The server includes comprehensive structured logging for monitoring AI tool usage and performance. The logging system is implemented via:

- **`main.go`**: `instrument()` wrapper function that wraps each tool handler to capture execution metrics
- **`duckdb_client.go`**: `LogQueryAsync()` function that asynchronously writes logs to DuckDB's `mcp_query_log` table

The logging system:

- Records timestamps, tool names, parameters, result counts, and query durations
- Provides asynchronous logging that doesn't block tool execution
- Stores logs in DuckDB for fast analytics via the `query_analytics` tool
- Supports optional persistent storage via `DUCKDB_PATH` environment variable

This enables better observability into how tools are being used and helps identify performance bottlenecks.

---

### db_info

Diagnostic tool that returns database connection info, PostgreSQL version, replication status, and upload counts. No parameters required.

---

### ping

Health check. Returns `"pong"`. No parameters required.

## REST API

The server also exposes a standard REST API on the same port as the MCP endpoints. All endpoints return JSON and are documented interactively via Swagger UI.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/radiation` | Find measurements near lat/lon |
| GET | `/api/area` | Find measurements in a bounding box |
| GET | `/api/tracks` | List bGeigie measurement tracks |
| GET | `/api/track/{id}` | Get measurements from a track |
| GET | `/api/device/{id}/history` | Device history (bGeigie + fixed sensors) |
| GET | `/api/sensors` | List active fixed sensors |
| GET | `/api/sensor/{id}/current` | Latest reading from a sensor |
| GET | `/api/sensor/{id}/history` | Time-series from a sensor |
| GET | `/api/spectra` | Browse gamma spectroscopy records |
| GET | `/api/spectrum/{marker_id}` | Full spectroscopy channel data |
| GET | `/api/stats` | Aggregate radiation statistics |
| GET | `/api/info/{topic}` | Reference information (units, safety levels, etc.) |
| GET | `/docs/` | Interactive Swagger UI |
| GET | `/docs/doc.json` | Raw OpenAPI spec |

**Quick example:**
```bash
curl "http://localhost:3333/api/radiation?lat=37.42&lon=141.03&radius_m=5000&limit=10"
```

### Updating API Documentation

The Swagger docs are generated from `// @Summary`, `// @Param`, and `// @Router` annotations in the `rest_*.go` files. After changing any annotation, regenerate with:

```bash
# Install swag CLI (one-time)
go install github.com/swaggo/swag/cmd/swag@latest

# Regenerate from go/ directory
cd go && swag init -g cmd/mcp-server/rest.go --dir cmd/mcp-server --output cmd/mcp-server/docs
```

The generated `docs/` folder is committed to the repo — the deployed binary does not need the swag CLI.

## Quick Start

```bash
cd go
go build -o safecast-mcp ./cmd/mcp-server/
./safecast-mcp
```

The server listens on port 3333 by default. It serves both MCP protocol endpoints and the REST API.

Open `http://localhost:3333/docs/` for the interactive Swagger UI.

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MCP_BASE_URL` | No | Base URL advertised by the SSE transport so clients know where to POST messages back (default: `http://localhost:3333`). Must **not** include `/mcp` — the server appends that automatically. |
| `DATABASE_URL` | No | PostgreSQL connection string. If not set, uses the Safecast REST API. |

### Endpoints

- **SSE**: `/mcp/sse` (GET) and `/mcp/message` (POST)
- **Streamable HTTP**: `/mcp-http` (POST)

## Connecting Claude to the MCP

### Using the Deployed Server

To connect Claude to the deployed Safecast MCP server:

1. Open [claude.ai](https://claude.ai) in your browser
2. Go to **Settings** (bottom-left) > **Integrations**
3. Click **Add more** > **Add custom integration**
4. Enter a name (e.g. "Safecast") and paste the Streamable HTTP endpoint URL:
   ```
   https://vps-01.safecast.jp/mcp-http
   ```
5. Click **Save** — the Safecast tools will now be available in your conversations

### Running Locally

To connect Claude to a locally running MCP server:

1. Start your local MCP server:
   ```bash
   cd go
   go run ./cmd/mcp-server/
   ```
   
2. The server will start on `http://localhost:3333` by default

3. Expose your local server to the internet using a tool like ngrok:
   ```bash
   ngrok http 3333
   ```
   
4. Take the HTTPS URL provided by ngrok (e.g., `https://abc123.ngrok.io`) and append `/mcp-http` to form the endpoint URL (e.g., `https://abc123.ngrok.io/mcp-http`)

5. Follow steps 2-5 from the "Using the Deployed Server" section above, using your ngrok URL instead

### Configuration Notes

- The MCP server supports both SSE and Streamable HTTP transports
- The Streamable HTTP endpoint (`/mcp-http`) is recommended for Claude integration
- If using a custom domain or different port, adjust the endpoint URL accordingly
- Make sure your server is accessible from the internet for Claude to connect

## Architecture

```
Claude / AI client
    |
    v
MCP Server (Go, mcp-go)
    |
    +---> Tool Execution with Structured Logging
          |
          +---> PostgreSQL + PostGIS (primary, if DATABASE_URL set)
          |     |
          |     +---> markers table (historical bGeigie data)
          |     |
          |     +---> realtime_measurements table (real-time sensor data)
          |     |
          |     +---> spectra table (spectroscopy data)
          |
          +---> DuckDB Analytics Engine
          |     |
          |     +---> Local usage statistics
          |
          +---> Structured Logging System
                |
                +---> Session tracking
                |
                +---> Performance metrics
                |
                +---> Error logging
                |
                +---> Optional external export
    |
    +---> simplemap.safecast.org REST API (fallback)
```

The server uses [`mcp-go`](https://github.com/mark3labs/mcp-go) for MCP protocol support. All tools attempt a direct database query first and fall back to the Safecast REST API if no database is configured or the query fails. Real-time data tools specifically query the `realtime_measurements` table for current sensor readings.

## Project Structure

```
go/cmd/mcp-server/
  main.go              # Server setup, tool registration, dual transport, instrumentation
  api_client.go        # Safecast REST API client
  db_client.go         # PostgreSQL connection pool (pgx)
  duckdb_client.go     # DuckDB analytics engine, async logging (LogQueryAsync)
  reference_data.go    # Static radiation reference data

  # MCP Tools
  tool_query_radiation.go
  tool_search_area.go
  tool_list_tracks.go
  tool_get_track.go
  tool_device_history.go
  tool_get_spectrum.go
  tool_list_spectra.go
  tool_radiation_info.go
  tool_list_sensors.go
  tool_sensor_current.go
  tool_sensor_history.go
  tool_analytics.go    # query_analytics, radiation_stats tools
  tool_db_info.go

  # REST API
  rest.go              # REST handler, Swagger UI, theme CSS
  rest_radiation.go
  rest_area.go
  rest_tracks.go
  rest_device.go
  rest_sensors.go
  rest_spectra.go
  rest_stats.go
  rest_info.go

  # Generated Documentation
  docs/
    docs.go            # Generated by swag init
    swagger.json
    swagger.yaml

  # Static Assets
  static/
    favicon.ico
    favicon-16x16.png
    favicon-32x32.png
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

## Deployment

Pushing to `main` automatically builds and deploys to the VPS via GitHub Actions (only when Go source files, `go.mod`/`go.sum`, or the workflow itself change).

### How it works

1. GitHub Action cross-compiles the Go binary
2. Uploads it to the VPS via rsync
3. Restarts the MCP server
4. Runs a health check against the `/mcp-http` endpoint

### Setting up secrets

The GitHub Action requires three repository secrets. Go to **Settings** > **Secrets and variables** > **Actions** and add:

| Secret | Description |
|--------|-------------|
| `SSH_PRIVATE_KEY` | SSH private key with access to the VPS (ed25519 format) |
| `VPS_HOST` | VPS hostname (e.g. `vps-01.safecast.jp`) |
| `DATABASE_URL` | PostgreSQL connection string |

To generate a deploy key:

```bash
ssh-keygen -t ed25519 -C "github-deploy@safecast-mcp" -f ~/.ssh/safecast-deploy -N ""
```

Then add the public key to the VPS:

```bash
ssh-copy-id -i ~/.ssh/safecast-deploy.pub root@vps-01.safecast.jp
```

And paste the contents of `~/.ssh/safecast-deploy` (the private key) as the `SSH_PRIVATE_KEY` secret in GitHub.

### Manual deploy

```bash
cd go
GOOS=linux GOARCH=amd64 go build -o safecast-mcp ./cmd/mcp-server/
scp safecast-mcp root@vps-01.safecast.jp:/root/safecast-mcp-server/
ssh root@vps-01.safecast.jp "systemctl restart safecast-mcp"
```

## Contributing

Contributions welcome. If changing a tool's interface, please open an issue first. Fork, branch, PR.

## License

MIT

## Technical Implementation Details

### CORS Processing
The application handles cross-origin resource sharing through standard HTTP headers managed by the underlying `mcp-go` library.

### Logarithmic Scaling
Used for mapping radiation values to visual properties like marker opacity and color intensity for better visualization of wide-ranging values.

### Timestamp Conversion
The application converts Unix timestamps from the API to human-readable formats for display in map popups.

### Responsive Design
The application adapts to various screen sizes using responsive CSS techniques.

## API Integration Details

- **Endpoints Used**: 
  - `/mcp/sse` (SSE transport)
  - `/mcp/message` (message transport) 
  - `/mcp-http` (streamable HTTP transport)
- **Data Types**: JSON responses containing radiation measurements, sensor information, and spectroscopy data
- **Request Methods**: Both GET (for SSE) and POST (for message passing) are supported

## Browser Compatibility Requirements

- HTML5/CSS3/ES5+ compatible browsers
- WebSocket support for SSE communication
- Modern JavaScript engine for processing MCP protocol messages

## Deployment Notes

- The application **must** be served via HTTP(S) server - direct file opening in browsers will not work due to CORS restrictions and API communication requirements.
- Requires a backend server to handle MCP protocol communication with AI clients like Claude.

<a href="https://www.paypal.com/ncp/payment/MAXS4ZUSGPDD4">
  <img src="https://safecast.org/wp-content/uploads/2024/08/Donation-PayPal-1.png" border="0" alt="Donate with PayPal" />
</a>