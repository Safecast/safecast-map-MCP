# Comprehensive Plan: DuckDB + PostgreSQL for Fast Analytics of MCP User Queries

## Overview
This plan outlines the integration of DuckDB with PostgreSQL to enable fast analytics of MCP user queries while maintaining the existing operational infrastructure. The solution leverages DuckDB's PostgreSQL extension to attach directly to the existing database and run analytical queries orders of magnitude faster than PostgreSQL for OLAP workloads.

## Architecture Design

```
┌─────────────────────────────────────────────────────────────────┐
│                    MCP Server (Go)                              │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  Tool Handlers                                          │    │
│  │  ├─ Core tools → pgx → PostgreSQL                      │    │
│  │  ├─ Analytics tools → DuckDB → PostgreSQL (via ATTACH) │    │
│  │  └─ Audit logger → DuckDB (local audit log)            │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                PostgreSQL + PostGIS                             │
│  - markers, uploads, spectra, realtime_measurements           │
│  - Optional: mcp_tool_log (tool usage logging)                │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                   DuckDB Analytics Engine                       │
│  - Fast analytical queries (OLAP)                              │
│  - Query audit logs                                            │
│  - Aggregated statistics                                       │
│  - Cached data for frequent queries                            │
└─────────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Infrastructure Setup
1. **Add DuckDB Dependency**
   - Add `github.com/marcboeker/go-duckdb` to the Go project
   - Update `go.mod` and `go.sum`

2. **Create DuckDB Client Module**
   - Create `go/cmd/mcp-server/duckdb_client.go`
   - Initialize DuckDB database connection
   - Create audit log table:
   ```sql
   CREATE TABLE IF NOT EXISTS mcp_query_log (
     id            BIGINT DEFAULT nextval('seq_query_log'),
     tool_name     VARCHAR,
     params        JSON,
     result_count  INTEGER,
     duration_ms   DOUBLE,
     client_info   VARCHAR,
     created_at    TIMESTAMPTZ DEFAULT now()
   );
   ```

### Phase 2: Query Auditing
3. **Instrument Existing Tool Handlers**
   - Wrap each tool handler with timing and logging functionality
   - Use goroutines and channels for non-blocking audit logging
   - Log tool name, parameters, result count, duration, and client info

4. **Implement Audit Logging Function**
   - Create `LogQuery(tool, params, resultCount, duration, clientInfo)` function
   - Ensure logging doesn't impact response times

### Phase 3: PostgreSQL Attachment
5. **Configure DuckDB PostgreSQL Extension**
   - Implement PostgreSQL attachment functionality in DuckDB
   - Use read-only connection for analytical queries
   - Handle connection string securely via environment variables

6. **Create Analytical Query Functions**
   - Functions to query PostgreSQL data through DuckDB
   - Support for aggregations, time-series analysis, and statistical queries

### Phase 4: New Analytical Tools
7. **Develop New MCP Tools for Analytics**
   - `query_analytics`: Query usage statistics and trends
   - `radiation_stats`: Aggregate radiation statistics by area/time
   - `data_coverage`: Measurement density and coverage analysis
   - `trending_areas`: Most-queried locations and topics

### Phase 5: Performance Optimization
8. **Implement Caching Strategies**
   - Optional: Cache frequently accessed data in DuckDB's columnar format
   - Set up periodic refresh mechanisms for cached data
   - Add environment variables to control caching behavior

9. **Optimize Query Patterns**
   - Push down predicates to PostgreSQL when possible
   - Use DuckDB for aggregations and complex analytics
   - Maintain PostgreSQL for spatial queries that leverage PostGIS

### Phase 6: Deployment & Monitoring
10. **Update Deployment Workflow**
    - Ensure DuckDB binary ships with CGo support
    - Update build process to handle CGo dependencies
    - Configure DuckDB file location via environment variables

11. **Add Monitoring and Fallback Mechanisms**
    - Graceful degradation if DuckDB initialization fails
    - Monitor query performance improvements
    - Track usage of new analytical tools

## Data Synchronization Strategy

The primary approach uses **direct attachment** without synchronization:
- DuckDB connects directly to PostgreSQL using the postgres extension
- Data is read on-demand from PostgreSQL
- No synchronization needed as data is read directly from the source

Optional caching layer for performance:
- Periodic refresh of frequently accessed data from PostgreSQL to DuckDB
- Background goroutine to refresh cached data at configurable intervals
- Event-based sync where changes in PostgreSQL trigger updates to cached data in DuckDB

## Technical Considerations

- **CGo Requirement**: The `go-duckdb` library requires CGo, so builds need `CGO_ENABLED=1`
- **Binary Size**: DuckDB adds ~30-40MB to the binary, which is acceptable for server deployment
- **Security**: Use read-only PostgreSQL user for DuckDB connections
- **Performance**: DuckDB excels at analytical queries, offering significant speedups for aggregations and scans
- **Fallback Strategy**: If DuckDB fails, analytical tools return "unavailable" while core tools continue working

## Benefits

- **Speed**: DuckDB runs analytical queries significantly faster than PostgreSQL for OLAP workloads
- **No Data Duplication**: Initially, no data copying is required (DuckDB reads directly from PostgreSQL)
- **Scalability**: Separates operational and analytical workloads
- **Compatibility**: Maintains existing PostgreSQL infrastructure for operational queries
- **Flexibility**: Supports both usage analytics and radiation data analytics

This implementation follows the existing patterns in the codebase and leverages the detailed plans already outlined in the documentation files, ensuring a smooth integration that enhances the analytics capabilities of the Safecast MCP server.