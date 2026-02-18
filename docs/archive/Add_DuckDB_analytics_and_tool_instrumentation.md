# Add DuckDB Analytics and Tool Instrumentation

## Overview

Commit `cd46ce52` introduces a sophisticated analytics system to the Safecast MCP server using DuckDB. This enhancement provides comprehensive monitoring, logging, and analytical capabilities for the server's operations.

## Key Components

### 1. DuckDB Integration (`duckdb_client.go`)

- **Database Connection**: Initializes DuckDB as an analytics engine that can attach to the PostgreSQL database
- **Audit Logging**: Creates an audit log table (`mcp_query_log`) to track all tool executions
- **Asynchronous Logging**: Implements non-blocking logging of tool usage with timing data
- **Analytics Functions**: Provides functions to query usage statistics and run analytical queries

### 2. Tool Instrumentation (`main.go`)

- **Wrapper Function**: Wraps all existing tools with timing and logging functionality
- **Execution Tracking**: Logs each tool execution with parameters, result count, and duration
- **Performance Monitoring**: Measures execution times for performance analysis

### 3. New Analytics Tools (`tool_analytics.go`)

- **`query_analytics`**: Returns usage statistics for all tools including call counts, average duration, and maximum duration
- **`radiation_stats`**: Provides aggregate radiation statistics from the Safecast database with configurable aggregation intervals (yearly, monthly, or overall)

### 4. Enhanced Configuration

- **`DUCKDB_PATH`**: Environment variable for persistent DuckDB storage (defaults to in-memory)
- **`MCP_PORT`**: Environment variable to customize the server port (defaults to 3333)

## How It Works

### Initialization Process
1. During server startup, DuckDB is initialized
2. Attempts to attach the PostgreSQL database specified by `DATABASE_URL` as a read-only source called `postgres_db`
3. Creates the audit log table if it doesn't exist

### Query Logging
Every tool execution is logged asynchronously to a local DuckDB audit table containing:
- Tool name
- Parameters used
- Result count
- Execution duration
- Client information

### Analytics Capabilities
The system can run analytical queries against both:
- Local audit logs (for usage statistics)
- The attached PostgreSQL database (for radiation data analytics)

### Performance Considerations
- Logging occurs in a goroutine to avoid blocking tool responses
- Minimal impact on performance due to asynchronous design

## Benefits

1. **Operational Visibility**: Detailed insights into tool usage patterns and performance
2. **Performance Monitoring**: Track execution times and identify slow-performing tools
3. **Data Analytics**: Enable advanced queries on radiation data without impacting the primary database
4. **Usage Statistics**: Monitor which tools are most frequently used
5. **Scalability**: Offload analytical queries to DuckDB, reducing load on the primary PostgreSQL database

## Technical Implementation

### DuckDB Configuration
```go
// Attach Postgres database
pgURL := os.Getenv("DATABASE_URL")
if pgURL != "" {
    query := fmt.Sprintf("ATTACH '%s' AS postgres_db (TYPE POSTGRES, READ_ONLY)", pgURL)
    if _, err := duckDB.Exec(query); err != nil {
        log.Printf("Warning: failed to attach postgres to duckdb: %v", err)
    } else {
        log.Println("Attached PostgreSQL database to DuckDB as 'postgres_db'")
    }
}
```

### Asynchronous Logging
```go
// LogQueryAsync logs a tool execution to DuckDB asynchronously
func LogQueryAsync(toolName string, params map[string]any, resultCount int, duration time.Duration, clientInfo string) {
    if duckDB == nil {
        return
    }

    go func() {
        paramsStr := fmt.Sprintf("%v", params)

        _, err := duckDB.Exec(`
            INSERT INTO mcp_query_log (tool_name, params, result_count, duration_ms, client_info)
            VALUES (?, ?, ?, ?, ?)
        `, toolName, paramsStr, resultCount, float64(duration.Milliseconds()), clientInfo)

        if err != nil {
            log.Printf("Error logging query to DuckDB: %v", err)
        }
    }()
}
```

## New Tools Added

### query_analytics
- **Description**: Get usage statistics for MCP tools (call counts, duration)
- **Powered by**: DuckDB local logs
- **Returns**: Tool name, call count, average duration, and maximum duration

### radiation_stats
- **Description**: Get aggregate radiation statistics from the Safecast database
- **Powered by**: DuckDB+Postgres
- **Parameters**: 
  - `interval`: Aggregation interval ('year', 'month', or 'overall')
- **Returns**: Aggregate statistics including count, average value, and maximum value

## Environment Variables

- `DATABASE_URL`: PostgreSQL connection string (used for attaching to DuckDB)
- `DUCKDB_PATH`: Path for persistent DuckDB storage (empty for in-memory)
- `MCP_PORT`: Port for the MCP server (defaults to 3333)

## Dependencies Added

- `github.com/marcboeker/go-duckdb`: DuckDB Go driver
- Various Apache Arrow dependencies for efficient data processing