package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
	_ "github.com/marcboeker/go-duckdb"
)

var duckDB *sql.DB

// initDuckDB initializes the DuckDB connection and attaches the Postgres database.
func initDuckDB() error {
	// Open DuckDB connection (in-memory)
    // Note: "?access_mode=READ_WRITE" or similar flags can be used, but default is fine.
    // If we want persistent logs, we might want a file path.
    // The plan says "DuckDB (local audit log)". 
    // Implementing purely in-memory for now as a robust default, 
    // but allowing a path via DUCKDB_PATH if set.
    
    duckPath := os.Getenv("DUCKDB_PATH")
    if duckPath == "" {
        duckPath = "" // empty DSN creates in‑memory DuckDB
    }

	var err error
	duckDB, err = sql.Open("duckdb", duckPath)
	if err != nil {
		return fmt.Errorf("failed to open duckdb: %w", err)
	}

	if err := duckDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping duckdb: %w", err)
	}

	// Install postgres extension (ignore error if already installed/bundled)
	duckDB.Exec("INSTALL postgres;")

    // Load postgres extension
    if _, err := duckDB.Exec("LOAD postgres;"); err != nil {
		return fmt.Errorf("failed to load postgres extension: %w", err)
	}

	// Attach Postgres database
    // We reuse DATABASE_URL. DuckDB's postgres scanner often accepts libpq connection strings.
	pgURL := os.Getenv("DATABASE_URL")
	if pgURL != "" {
		// Attach as 'postgres_db'
        // DuckDB 0.9+ supports ATTACH '...' AS name (TYPE POSTGRES)
		query := fmt.Sprintf("ATTACH '%s' AS postgres_db (TYPE POSTGRES, READ_ONLY)", pgURL)
		if _, err := duckDB.Exec(query); err != nil {
            // Fallback - maybe it's already attached or error in string format
			log.Printf("Warning: failed to attach postgres to duckdb: %v", err)
            // We don't return error here to allow running with just local analytics if postgres fails
		} else {
            log.Println("Attached PostgreSQL database to DuckDB as 'postgres_db'")
        }
	} else {
        log.Println("Warning: DATABASE_URL not set, skipping Postgres attachment")
    }

	// Create sequence first, then the audit log table that references it
	createSchemaQuery := `
	CREATE SEQUENCE IF NOT EXISTS seq_query_log;
	CREATE TABLE IF NOT EXISTS mcp_query_log (
		id            BIGINT DEFAULT nextval('seq_query_log'),
		tool_name     VARCHAR,
		params        JSON,
		result_count  INTEGER,
		duration_ms   DOUBLE,
		client_info   VARCHAR,
		created_at    TIMESTAMPTZ DEFAULT now()
	);
	`
	if _, err := duckDB.Exec(createSchemaQuery); err != nil {
		return fmt.Errorf("failed to create audit log schema: %w", err)
	}

	return nil
}

// LogQueryAsync logs a tool execution to DuckDB asynchronously.
func LogQueryAsync(toolName string, params map[string]any, resultCount int, duration time.Duration, clientInfo string) {
    if duckDB == nil {
        return
    }
    
    go func() {
        // Serialize params as proper JSON for DuckDB's JSON column type.
        paramsJSON, err := json.Marshal(params)
        if err != nil {
            log.Printf("Error marshaling params to JSON: %v", err)
            return
        }
        paramsStr := string(paramsJSON)

        _, execErr := duckDB.Exec(`
            INSERT INTO mcp_query_log (tool_name, params, result_count, duration_ms, client_info)
            VALUES (?, ?, ?, ?, ?)
        `, toolName, paramsStr, resultCount, float64(duration.Milliseconds()), clientInfo)

        if execErr != nil {
            log.Printf("Error logging query to DuckDB: %v", execErr)
        }
    }()
}

// Analytics Functions

// GetToolUsageStats returns usage statistics for tools.
func GetToolUsageStats() ([]map[string]any, error) {
    if duckDB == nil {
        return nil, fmt.Errorf("duckdb not initialized")
    }
    
    rows, err := duckDB.Query(`
        SELECT tool_name, COUNT(*) as calls, AVG(duration_ms) as avg_duration, MAX(duration_ms) as max_duration
        FROM mcp_query_log
        GROUP BY tool_name
        ORDER BY calls DESC
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var stats []map[string]any
    for rows.Next() {
        var toolName string
        var calls int64
        var avgDur, maxDur float64
        if err := rows.Scan(&toolName, &calls, &avgDur, &maxDur); err != nil {
            return nil, err
        }
        stats = append(stats, map[string]any{
            "tool": toolName,
            "calls": calls,
            "avg_ms": avgDur,
            "max_ms": maxDur,
        })
    }
    return stats, nil
}

// QueryPostgresAnalytics executes an arbitrary analytical query on the attached Postgres DB.
// This is the powerful "FAQ" enabler.
// WARNING: Logic constraints should be applied in a real production environment.
func QueryPostgresAnalytics(query string, args ...any) ([]map[string]any, error) {
    if duckDB == nil {
        return nil, fmt.Errorf("duckdb not initialized")
    }
    
    // We execute the query directly against DuckDB, which can reference postgres_db.tables
    rows, err := duckDB.Query(query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    // Generic map scanning
    cols, err := rows.Columns()
    if err != nil {
        return nil, err
    }
    
    var results []map[string]any
    for rows.Next() {
        // Create a slice of interface{} to hold values
        columns := make([]interface{}, len(cols))
        columnPointers := make([]interface{}, len(cols))
        for i := range columns {
            columnPointers[i] = &columns[i]
        }

        if err := rows.Scan(columnPointers...); err != nil {
            return nil, err
        }

        row := make(map[string]any)
        for i, colName := range cols {
            val := columns[i]
            // DuckDB driver might return specific types, handle basic conversion if needed
            // For now, pass through
            if b, ok := val.([]byte); ok {
                row[colName] = string(b)
            } else {
                row[colName] = val
            }
        }
        results = append(results, row)
    }
    return results, nil
}
