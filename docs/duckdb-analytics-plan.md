# DuckDB + PostgreSQL Extension for MCP Query Analytics

## Current State

- Go MCP server with **pgx/v5** connecting to PostgreSQL + PostGIS
- 8 tools querying 4 tables (`markers`, `uploads`, `spectra`, `realtime_measurements`)
- **No analytics or audit logging** — all query activity is ephemeral

## What DuckDB Brings

DuckDB's `postgres` extension can **attach directly to your existing Postgres** and run analytical (OLAP) queries orders of magnitude faster than Postgres for scans/aggregations, while Postgres stays the source of truth. DuckDB runs **in-process** (no separate server) and stores data in columnar format.

---

## Phase 1: Query Audit Log (DuckDB-native)

Log every MCP tool invocation into a local DuckDB file for analysis.

**New file: `go/cmd/mcp-server/duckdb_client.go`**

- Init DuckDB database (e.g., `/var/data/safecast-analytics.duckdb`)
- Create audit table:

```sql
CREATE TABLE IF NOT EXISTS mcp_query_log (
  id          BIGINT DEFAULT nextval('seq_query_log'),
  tool_name   VARCHAR,
  params      JSON,
  result_count INTEGER,
  duration_ms DOUBLE,
  client_info VARCHAR,
  created_at  TIMESTAMPTZ DEFAULT now()
);
```

- Expose `LogQuery(tool, params, resultCount, duration)` function

**Instrument each tool handler** — wrap the existing tool calls to capture timing + result count and call `LogQuery()` asynchronously (fire-and-forget via goroutine + channel so it doesn't slow down responses).

---

## Phase 2: Attach PostgreSQL from DuckDB

Use DuckDB's `postgres` extension to query Postgres data analytically without replication.

```sql
INSTALL postgres;
LOAD postgres;
ATTACH 'dbname=safecast host=... user=...' AS pg (TYPE POSTGRES, READ_ONLY);
```

This lets DuckDB run analytical queries directly against Postgres tables (`pg.markers`, `pg.uploads`, etc.) using its vectorized engine. Great for:

- Full-table aggregations (avg dose rate by region/month)
- Time-series rollups
- Cross-table analytics that would be slow in Postgres

---

## Phase 3: New Analytical MCP Tools

Add new tools powered by DuckDB that expose insights to MCP users:

| Tool | Purpose | Query Source |
|------|---------|-------------|
| `query_analytics` | "How many queries hit tool X this week?" | DuckDB native (audit log) |
| `radiation_stats` | Aggregate stats: avg/min/max/p95 dose rates by area/time | DuckDB → Postgres via attach |
| `data_coverage` | Heatmap-style coverage: measurement density by region | DuckDB → Postgres via attach |
| `trending_areas` | Most-queried locations from audit log | DuckDB native |

Example — `radiation_stats`:

```sql
SELECT
  date_trunc('month', to_timestamp(date)) AS month,
  round(avg(doserate), 4) AS avg_usv,
  round(max(doserate), 4) AS max_usv,
  count(*) AS measurements
FROM pg.markers
WHERE lat BETWEEN $1 AND $2 AND lon BETWEEN $3 AND $4
GROUP BY 1 ORDER BY 1;
```

This kind of aggregation over millions of rows is where DuckDB shines vs. Postgres.

---

## Phase 4: Optional Local Caching

For repeated analytical patterns, materialize hot data into DuckDB's native columnar storage:

```sql
-- Periodic refresh (e.g., every hour via goroutine ticker)
CREATE OR REPLACE TABLE markers_cache AS
  SELECT id, doserate, date, lat, lon, device_id, detector, trackid
  FROM pg.markers
  WHERE date > epoch(now()) - 86400*90;  -- last 90 days
```

Benefits:

- Sub-millisecond aggregations on cached data
- No load on Postgres for analytical queries
- Automatic staleness management via refresh interval

---

## Implementation Steps

1. **Add `go-duckdb` dependency** — `github.com/marcboeker/go-duckdb` (CGo-based, uses `database/sql` interface)
2. **Create `duckdb_client.go`** — init, attach postgres, audit table
3. **Add async query logger** — channel-based, non-blocking
4. **Instrument tool handlers** — wrap each with timing/logging (minimal diff per file)
5. **Add analytical tools** — new `tool_query_analytics.go`, `tool_radiation_stats.go`
6. **Add cache refresh goroutine** — optional, controlled by env var
7. **Update deploy workflow** — DuckDB needs the binary to ship with CGo support (or use static build)

---

## Dependencies & Considerations

| Concern | Approach |
|---------|----------|
| **CGo requirement** | `go-duckdb` requires CGo. Build with `CGO_ENABLED=1`. Cross-compile may need Docker. |
| **DuckDB file location** | Env var `DUCKDB_PATH` (default: `./analytics.duckdb`). Can also run in-memory with `:memory:` for no persistence. |
| **Postgres extension install** | DuckDB auto-downloads extensions. On VPS, ensure outbound HTTPS or pre-bundle the extension. |
| **PostGIS functions** | DuckDB's postgres attach can call PostGIS functions but they execute on Postgres side. Spatial analytics should push predicates to Postgres. |
| **Binary size** | DuckDB adds ~30-40MB to the binary. Acceptable for a server deployment. |
| **Fallback** | If DuckDB init fails, analytics tools return "unavailable" but core tools continue working (same pattern as current DB fallback). |

---

## Architecture After Integration

```
MCP Client (Claude, etc.)
        ↓
   MCP Server (Go)
        ↓
   ┌─────────────────────────────────┐
   │  Tool Handlers                  │
   │  ├─ Core tools → pgx → Postgres│  (unchanged)
   │  ├─ Analytics tools → DuckDB ──┤──→ Postgres (via ATTACH)
   │  └─ Audit logger → DuckDB     │  (local columnar storage)
   └─────────────────────────────────┘
```
