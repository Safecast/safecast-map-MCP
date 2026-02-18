# Plan: DuckDB + Postgres for Fast Analysis of MCP Queries

**Status:** ✅ IMPLEMENTED (See ../../README.md for current documentation)

> **Implementation**: DuckDB analytics engine is now integrated with Postgres attachment for radiation data analytics (`radiation_stats` tool) and local logging for usage analytics (`query_analytics` tool). See `duckdb_client.go` and `tool_analytics.go`.

This document outlines a plan for using **DuckDB** together with your existing **PostgreSQL** database to support fast analytics—either over **MCP tool usage** (who called which tools, when, with what params) or over the **Safecast radiation data** itself, without slowing down the live MCP server.

**Important:** There is no DuckDB extension that runs *inside* Postgres. The standard approach is the other way around: **DuckDB connects to Postgres** via its `postgres` extension (postgres_scanner) and runs analytical queries in DuckDB, reading from Postgres on demand. That’s what this plan is based on.

---

## 1. Two Analytics Targets

| Target | Purpose | Data source |
|--------|--------|-------------|
| **A. MCP query / usage analytics** | Understand how users use the MCP (tools, params, frequency, errors). | New: log table in Postgres (or files) populated by the Go MCP server. |
| **B. Safecast data analytics** | Heavy aggregations, time series, stats over `markers` / `uploads` without loading Postgres. | Existing Postgres + PostGIS (read via DuckDB’s postgres extension). |

You can do one or both. The rest of the plan supports both.

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  MCP Server (Go)                                                 │
│  - Serves tools (query_radiation, search_area, list_tracks, …)   │
│  - Optional: logs each tool call → Postgres (mcp_tool_log)        │
└────────────────────────────┬────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  PostgreSQL + PostGIS                                             │
│  - markers, uploads, etc. (Safecast data)                        │
│  - Optional: mcp_tool_log (tool, params, ts, duration, …)         │
└────────────────────────────┬────────────────────────────────────┘
                              │
                              │  DuckDB postgres extension
                              │  (ATTACH; read-only queries)
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  DuckDB (analytics)                                               │
│  - Ad‑hoc SQL, dashboards, exports                                │
│  - Reads from Postgres (no copy) or from exported Parquet/CSV    │
└─────────────────────────────────────────────────────────────────┘
```

- **Live traffic:** MCP server → Postgres only (unchanged).
- **Analytics:** You run DuckDB (CLI, Python, or a small script) and attach to Postgres; DuckDB runs analytical SQL and pulls only what it needs.

---

## 3. Option A: Analyzing MCP Tool Usage

### 3.1 Log tool calls in the MCP server

- In the Go server, after each tool handler runs (or in a shared wrapper), log:
  - `tool_name`, `arguments` (JSON or key fields), `started_at`, `finished_at`, `duration_ms`, `success`, optional `error_message`, optional `client_id` if you have it.
- **Storage options:**
  - **Postgres table** (e.g. `mcp_tool_log`): simple, one place for logs and analytics; DuckDB can read it via the postgres extension.
  - **File-based (e.g. JSONL/Parquet):** write logs to a file or object store; DuckDB can query those directly (no Postgres table needed).

**Minimal Postgres schema (if you choose Postgres for logs):**

```sql
CREATE TABLE IF NOT EXISTS mcp_tool_log (
  id           BIGSERIAL PRIMARY KEY,
  tool_name    TEXT NOT NULL,
  arguments    JSONB,
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ,
  duration_ms  INT,
  success      BOOLEAN NOT NULL,
  error_message TEXT
);

-- Optional indexes for common analytics
CREATE INDEX idx_mcp_tool_log_tool_started ON mcp_tool_log (tool_name, started_at);
CREATE INDEX idx_mcp_tool_log_started ON mcp_tool_log (started_at);
```

- Keep retention in mind (e.g. partition or delete old rows) so the table doesn’t grow unbounded.

### 3.2 Analyze with DuckDB

- Install DuckDB and load the postgres extension; attach your Postgres DB (see section 5).
- Query the log table with standard SQL, e.g.:
  - Tools by call count, by day/week.
  - P95/P99 duration per tool.
  - Error rate by tool.
  - Most common `query_radiation` / `search_area` params (if you store them in `arguments`).

No need to copy data: DuckDB will read from Postgres when you run these queries.

---

## 4. Option B: Analyzing Safecast Data (markers, uploads)

### 4.1 Use DuckDB as the analytics client

- Keep **all live MCP tool traffic** on Postgres + PostGIS (spatial indexes, `ST_DWithin`, etc.). Do not route those requests through DuckDB.
- Use **DuckDB only for analytics** (reports, dashboards, one-off aggregations):
  - Attach Postgres with the postgres extension.
  - Run SQL in DuckDB that references Postgres tables, e.g. `FROM postgres_db.main.markers`, possibly with filters (date range, region) to limit data transfer.
  - DuckDB will pull data from Postgres and run aggregations in its columnar engine.

### 4.2 What to watch for

- **PostGIS types:** DuckDB’s postgres extension can read geometry as raw bytes or as a supported type depending on version. For complex spatial analytics you might:
  - Do spatial filters in Postgres (e.g. create a materialized view or a table with pre-filtered rows) and then aggregate in DuckDB, or
  - Export filtered subsets to Parquet and run spatial work in DuckDB’s spatial extension, or
  - Keep heavy spatial queries in Postgres and use DuckDB for non-spatial aggregations (counts, time series, device stats).
- **Volume:** For very large scans, consider periodic exports (e.g. nightly) of aggregated or filtered data to Parquet and point DuckDB at Parquet for the heaviest reports, to avoid long-running connections to Postgres.

---

## 5. DuckDB + Postgres Setup (postgres extension)

### 5.1 Install and attach

In DuckDB (CLI or embedded):

```sql
INSTALL postgres;
LOAD postgres;

ATTACH 'host=localhost port=5432 user=readonly dbname=safecast password=...' AS postgres_db (TYPE postgres);
```

Use a **read-only** Postgres user for analytics so you don’t risk writes.

### 5.2 Query Postgres tables from DuckDB

- Tables appear under the attached schema, e.g. `postgres_db.main.markers`, `postgres_db.main.uploads`, and (if you added it) `postgres_db.main.mcp_tool_log`.
- Example (option B – Safecast data):

```sql
SELECT
  date_trunc('month', to_timestamp(date)) AS month,
  count(*) AS measurements,
  avg(doserate) AS avg_doserate
FROM postgres_db.main.markers
WHERE date >= extract(epoch from date '2024-01-01')
GROUP BY 1
ORDER BY 1;
```

- Example (option A – MCP usage):

```sql
SELECT
  tool_name,
  count(*) AS calls,
  avg(duration_ms) AS avg_ms
FROM postgres_db.main.mcp_tool_log
WHERE started_at >= current_date - 30
GROUP BY tool_name
ORDER BY calls DESC;
```

### 5.3 Connection string and security

- Prefer environment variables or a config file for the connection string; avoid committing secrets.
- On the VPS or wherever DuckDB runs, ensure network access to Postgres (localhost or allowed IP) and that the Postgres user has only `SELECT` on the needed schemas/tables.

---

## 6. Implementation Phases

| Phase | Scope | Effort |
|-------|--------|--------|
| **1. DuckDB + Postgres only (option B)** | Install DuckDB, load postgres extension, ATTACH to existing Postgres, run ad‑hoc analytical queries over `markers`/`uploads`. No Go changes. | Low |
| **2. MCP usage logging (option A)** | Add `mcp_tool_log` table; in Go, log each tool call (tool name, params, duration, success). Optional: middleware or wrapper so all tools are logged in one place. | Medium |
| **3. DuckDB for usage analytics** | Use DuckDB + postgres to query `mcp_tool_log` (counts, latency percentiles, errors). Add a small script or notebook for recurring reports. | Low |
| **4. Optional: export/sync** | If needed, periodic export of aggregated or filtered data from Postgres to Parquet and query Parquet from DuckDB to reduce load on Postgres for very heavy jobs. | Medium |

---

## 7. Summary

- **DuckDB “with” Postgres** here means: **DuckDB connects to Postgres** (postgres extension / ATTACH) and runs analytical SQL; Postgres remains the source of truth and serves the MCP server.
- **MCP query analysis:** Add a log table (or log files), write tool calls from the Go server, then analyze with DuckDB reading from Postgres or from files.
- **Safecast data analysis:** Use DuckDB as an analytics client attached to Postgres; keep live MCP traffic on Postgres/PostGIS; use DuckDB for aggregations and reporting, and optionally Parquet for the heaviest workloads.

If you tell me whether you care more about (A) usage analytics or (B) radiation data analytics (or both), the next step can be a concrete schema + example queries or a minimal Go logging patch.
