package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// Tool Definitions

var queryAnalyticsToolDef = mcp.NewTool("query_analytics",
	mcp.WithDescription("Get usage statistics for MCP tools (call counts, duration). Powered by DuckDB local logs. IMPORTANT: Every response includes an _ai_generated_note field. You MUST display this note verbatim to the user in every response that uses data from this tool."),
)

var radiationStatsToolDef = mcp.NewTool("radiation_stats",
	mcp.WithDescription("Get aggregate radiation statistics from the Safecast database (e.g., average dose rate by year/month). Powered by DuckDB+Postgres. IMPORTANT: Every response includes an _ai_generated_note field. You MUST display this note verbatim to the user in every response that uses data from this tool."),
	mcp.WithString("interval",
		mcp.Description("Aggregation interval: 'year', 'month', or 'overall'"),
		mcp.Enum("year", "month", "overall"),
		mcp.DefaultString("year"),
	),
)

// Handlers

func handleQueryAnalytics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if duckDB == nil {
		return mcp.NewToolResultError("DuckDB analytics engine is not initialized"), nil
	}

	const analyticsQuery = `
		SELECT tool_name, COUNT(*) as count, 
               AVG(duration_ms) as avg_ms, 
               MAX(duration_ms) as max_ms
		FROM mcp_query_log
		GROUP BY tool_name
		ORDER BY count DESC
	`

	rows, err := duckDB.Query(analyticsQuery)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Query failed: %v", err)), nil
	}
	defer rows.Close()

	var stats []map[string]any
	for rows.Next() {
		var tool string
		var count int64
		var avgMs, maxMs float64
		if err := rows.Scan(&tool, &count, &avgMs, &maxMs); err != nil {
			continue
		}
		stats = append(stats, map[string]any{
			"tool":    tool,
			"calls":   count,
			"avg_ms":  avgMs,
			"max_ms":  maxMs,
		})
	}

	return jsonResult(map[string]any{
		"stats": stats,
		"source": "duckdb_local_log",
		"_ai_generated_note": "This data was retrieved by an AI assistant using Safecast tools. The interpretation and presentation of this data may be influenced by the AI system.",
	})
}

func handleRadiationStats(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if duckDB == nil {
		return mcp.NewToolResultError("DuckDB analytics engine is not initialized"), nil
	}

	interval := req.GetString("interval", "year")

	var query string
	switch interval {
	case "year":
		// Query attached Postgres DB
        // Note: 'postgres_db' is the name we attached it as in duckdb_client.go
		query = `
			SELECT 
                EXTRACT(YEAR FROM captured_at) as year,
                COUNT(*) as count,
                AVG(value) as avg_value,
                MAX(value) as max_value
			FROM postgres_db.markers 
            WHERE value < 1000 -- filter outliers/test data if needed
			GROUP BY 1
			ORDER BY 1 DESC
            LIMIT 20
		`
	case "month":
		query = `
			SELECT 
                DATE_TRUNC('month', captured_at) as month,
                COUNT(*) as count,
                AVG(value) as avg_value
			FROM postgres_db.markers 
            WHERE captured_at > NOW() - INTERVAL '1 year'
			GROUP BY 1
			ORDER BY 1 DESC
		`
	default: // overall
		query = `
			SELECT 
                COUNT(*) as count,
                AVG(value) as avg_value,
                MAX(value) as max_value
			FROM postgres_db.markers
		`
	}

	// Execute against DuckDB which proxies to Postgres
	rows, err := duckDB.Query(query)
	if err != nil {
        // Provide helpful error if table doesn't exist (e.g. schema mismatch)
		return mcp.NewToolResultError(fmt.Sprintf("Analytics query failed (check if postgres is attached): %v", err)), nil
	}
	defer rows.Close()

    // Generic scanner for results
	cols, _ := rows.Columns()
	var results []map[string]any
    
	for rows.Next() {
        // Create generic pointers
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		row := make(map[string]any)
		for i, colName := range cols {
            val := columns[i]
            // Handle byte arrays (often strings in db drivers)
            if b, ok := val.([]byte); ok {
                row[colName] = string(b)
            } else {
			    row[colName] = val
            }
		}
		results = append(results, row)
	}

	return jsonResult(map[string]any{
		"interval": interval,
		"data":     results,
		"source":   "duckdb_postgres_attach",
		"_ai_generated_note": "This data was retrieved by an AI assistant using Safecast tools. The interpretation and presentation of this data may be influenced by the AI system.",
	})
}
