package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// Tool Definition

var queryExtremeReadingsToolDef = mcp.NewTool("query_extreme_readings",
	mcp.WithDescription("Find the highest or lowest radiation readings in the database with full location details. Use this to identify extreme measurements globally or within a specific region."),
	mcp.WithString("direction",
		mcp.Description("'highest' for maximum readings or 'lowest' for minimum readings"),
		mcp.Enum("highest", "lowest"),
		mcp.DefaultString("highest"),
	),
	mcp.WithNumber("limit",
		mcp.Description("Number of readings to return (1-100)"),
	),
	mcp.WithNumber("min_lat",
		mcp.Description("Southern boundary for optional geographic filter"),
	),
	mcp.WithNumber("max_lat",
		mcp.Description("Northern boundary for optional geographic filter"),
	),
	mcp.WithNumber("min_lon",
		mcp.Description("Western boundary for optional geographic filter"),
	),
	mcp.WithNumber("max_lon",
		mcp.Description("Eastern boundary for optional geographic filter"),
	),
)

// Handler

func handleQueryExtremeReadings(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if duckDB == nil {
		return mcp.NewToolResultError("DuckDB analytics engine is not initialized"), nil
	}

	direction := req.GetString("direction", "highest")
	limit := req.GetInt("limit", 10)
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Build query
	orderDir := "DESC"
	if direction == "lowest" {
		orderDir = "ASC"
	}

	// Check for geographic filter
	hasGeoFilter := false
	minLat := req.GetFloat("min_lat", -90)
	maxLat := req.GetFloat("max_lat", 90)
	minLon := req.GetFloat("min_lon", -180)
	maxLon := req.GetFloat("max_lon", 180)

	if minLat != -90 || maxLat != 90 || minLon != -180 || maxLon != 180 {
		hasGeoFilter = true
	}

	var query string
	if hasGeoFilter {
		query = fmt.Sprintf(`
			SELECT
				id,
				doserate,
				latitude,
				longitude,
				device_id,
				to_timestamp(date)::TIMESTAMP AS captured_at,
				track_id,
				detector
			FROM postgres_db.public.markers
			WHERE doserate > 0 AND doserate < 10000
			  AND latitude BETWEEN %.6f AND %.6f
			  AND longitude BETWEEN %.6f AND %.6f
			ORDER BY doserate %s
			LIMIT %d
		`, minLat, maxLat, minLon, maxLon, orderDir, limit)
	} else {
		query = fmt.Sprintf(`
			SELECT
				id,
				doserate,
				latitude,
				longitude,
				device_id,
				to_timestamp(date)::TIMESTAMP AS captured_at,
				track_id,
				detector
			FROM postgres_db.public.markers
			WHERE doserate > 0 AND doserate < 10000
			ORDER BY doserate %s
			LIMIT %d
		`, orderDir, limit)
	}

	// Execute query
	rows, err := duckDB.Query(query)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Query failed: %v", err)), nil
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id int64
		var doserate float64
		var lat, lon float64
		var deviceID, trackID, detector *string
		var capturedAt any

		if err := rows.Scan(&id, &doserate, &lat, &lon, &deviceID, &capturedAt, &trackID, &detector); err != nil {
			continue
		}

		result := map[string]any{
			"id":       id,
			"value":    doserate,
			"unit":     "µSv/h",
			"location": map[string]float64{"lat": lat, "lon": lon},
		}

		if capturedAt != nil {
			result["captured_at"] = fmt.Sprintf("%v", capturedAt)
		}
		if deviceID != nil {
			result["device_id"] = *deviceID
		}
		if trackID != nil {
			result["track_id"] = *trackID
		}
		if detector != nil {
			result["detector"] = *detector
		}

		results = append(results, result)
	}

	return jsonResult(map[string]any{
		"direction":          direction,
		"readings":           results,
		"count":              len(results),
		"source":             "duckdb_postgres_attach",
		"_ai_hint":           "CRITICAL INSTRUCTIONS: (1) The 'unit' field indicates measurement units - CPM means 'counts per minute' NOT 'counts per second'. Always interpret and report CPM values as counts per minute. (2) Present all data in a purely scientific, factual manner. NEVER use personal pronouns (I, we, I'll, I'm, you, your), exclamations (!, Perfect, Great, Excellent), or conversational phrases. Format responses as objective statements: 'Latest reading: X CPM at location Y' NOT 'I found a reading of X CPM' or 'Perfect! The sensor shows...'. State only objective facts and measurements. (3) Make location coordinates clickable links to the map: https://simplemap.safecast.org/?lat=LAT&lon=LON&zoom=15",
		"_ai_generated_note": "This data was retrieved by an AI assistant using Safecast tools. The interpretation and presentation of this data may be influenced by the AI system.",
	})
}
