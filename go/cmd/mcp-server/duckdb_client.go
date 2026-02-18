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
	// Configure DuckDB path: use DUCKDB_PATH env var or fallback to local file
	duckPath := os.Getenv("DUCKDB_PATH")
	if duckPath == "" {
		duckPath = "./analytics.duckdb"
	}
	
	// Enable concurrent access with READ_WRITE mode
	dsn := duckPath + "?access_mode=READ_WRITE"
	
	var err error
	duckDB, err = sql.Open("duckdb", dsn)
	if err != nil {
		return fmt.Errorf("failed to open duckdb: %w", err)
	}    

	if err := duckDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping duckdb: %w", err)
	}
	
	log.Printf("DuckDB initialized at %s", duckPath)

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

	// Create schema version tracking table
	schemaVersionQuery := `
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER
	);
	`
	if _, err := duckDB.Exec(schemaVersionQuery); err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}
	
	// Initialize schema version if empty
	var versionCount int
	err = duckDB.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&versionCount)
	if err == nil && versionCount == 0 {
		if _, err := duckDB.Exec("INSERT INTO schema_version (version) VALUES (1)"); err != nil {
			log.Printf("Warning: failed to initialize schema version: %v", err)
		}
	}

	// Create Audit Log Table (tool-level analytics: instrument(), LogQueryAsync)
    createTableQuery := `
    CREATE SEQUENCE IF NOT EXISTS seq_query_log;
    
    CREATE TABLE IF NOT EXISTS mcp_query_log (
        id BIGINT DEFAULT nextval('seq_query_log'),
        tool_name VARCHAR,
        params JSON,
        result_count INTEGER,
        duration_ms DOUBLE,
        client_info VARCHAR,
        created_at TIMESTAMPTZ DEFAULT now()
    );
    `    
	if _, err := duckDB.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create audit log table: %w", err)
	}

	// AI session log table (ai_logging.go: insertQueryLog). Same DuckDB, separate table to avoid breaking existing analytics.
	createAILogQuery := `
	CREATE TABLE IF NOT EXISTS mcp_ai_query_log (
		session_id      TEXT,
		timestamp       TIMESTAMP,
		tool_name       TEXT,
		generated_query TEXT,
		duration_ms      BIGINT,
		commit_hash     TEXT,
		error           TEXT
	);
	`
	if _, err := duckDB.Exec(createAILogQuery); err != nil {
		return fmt.Errorf("failed to create AI query log table: %w", err)
	}
	
	// Create indexes for production performance
	indexQueries := []string{
		"CREATE INDEX IF NOT EXISTS idx_ai_query_log_timestamp ON mcp_ai_query_log(timestamp);",
		"CREATE INDEX IF NOT EXISTS idx_ai_query_log_tool_name ON mcp_ai_query_log(tool_name);",
	}
	for _, idxQuery := range indexQueries {
		if _, err := duckDB.Exec(idxQuery); err != nil {
			log.Printf("Warning: failed to create index: %v", err)
			// Don't fail initialization if index creation fails (indexes may already exist)
		}
	}

	return nil
}

// LogQueryAsync logs a tool execution to DuckDB asynchronously.
func LogQueryAsync(toolName string, params map[string]any, resultCount int, duration time.Duration, clientInfo string) {
    if duckDB == nil {
        return
    }
    
    go func() {
        // Proper JSON serialization for params field
        var paramsJSON []byte
        if params != nil {
            var err error
            paramsJSON, err = json.Marshal(params)
            if err != nil {
                log.Printf("Error marshaling params to JSON: %v", err)
                paramsJSON = []byte("{}")
            }
        } else {
            paramsJSON = []byte("{}")
        }

        _, err := duckDB.Exec(`
            INSERT INTO mcp_query_log (tool_name, params, result_count, duration_ms, client_info)
            VALUES (?, ?, ?, ?, ?)
        `, toolName, string(paramsJSON), resultCount, float64(duration.Milliseconds()), clientInfo)
        
        if err != nil {
            log.Printf("Error logging query to DuckDB: %v", err)
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
