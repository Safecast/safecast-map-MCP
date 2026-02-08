package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

var listTracksToolDef = mcp.NewTool("list_tracks",
	mcp.WithDescription("Browse bGeigie Import tracks (bulk radiation measurement drives). Can filter by year and optionally month."),
	mcp.WithNumber("year",
		mcp.Description("Filter by year (e.g., 2024)"),
		mcp.Min(2000), mcp.Max(2100),
	),
	mcp.WithNumber("month",
		mcp.Description("Filter by month (1-12, requires year parameter)"),
		mcp.Min(1), mcp.Max(12),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of results to return (default: 50, max: 200)"),
		mcp.Min(1), mcp.Max(200),
		mcp.DefaultNumber(50),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleListTracks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	year := req.GetInt("year", 0)
	month := req.GetInt("month", 0)
	limit := req.GetInt("limit", 50)

	if month != 0 && year == 0 {
		return mcp.NewToolResultError("Month filter requires year parameter"), nil
	}
	if year != 0 && (year < 2000 || year > 2100) {
		return mcp.NewToolResultError("Year must be between 2000 and 2100"), nil
	}
	if month != 0 && (month < 1 || month > 12) {
		return mcp.NewToolResultError("Month must be between 1 and 12"), nil
	}
	if limit < 1 || limit > 200 {
		return mcp.NewToolResultError("Limit must be between 1 and 200"), nil
	}

	if dbAvailable() {
		return listTracksDB(ctx, year, month, limit)
	}
	return listTracksAPI(ctx, year, month, limit)
}

func listTracksDB(ctx context.Context, year, month, limit int) (*mcp.CallToolResult, error) {
	// Get distinct tracks with their stats from markers table
	query := `
		SELECT trackid,
			count(*) AS measurement_count,
			min(to_timestamp(date)) AS first_measurement,
			max(to_timestamp(date)) AS last_measurement,
			min(lat) AS min_lat, max(lat) AS max_lat,
			min(lon) AS min_lon, max(lon) AS max_lon,
			avg(doserate) AS avg_doserate
		FROM markers
		WHERE trackid IS NOT NULL AND trackid != ''`

	args := []any{}
	argIdx := 1

	if year != 0 {
		startDate := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		endDate := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		if month != 0 {
			startDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Unix()
			if month == 12 {
				endDate = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
			} else {
				endDate = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC).Unix()
			}
		}
		query += fmt.Sprintf(" AND date >= $%d AND date < $%d", argIdx, argIdx+1)
		args = append(args, startDate, endDate)
		argIdx += 2
	}

	query += " GROUP BY trackid ORDER BY max(date) DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Get total count
	countQuery := `SELECT count(DISTINCT trackid) AS total FROM markers WHERE trackid IS NOT NULL AND trackid != ''`
	countRow, _ := queryRow(ctx, countQuery)
	total := 0
	if countRow != nil {
		if t, ok := countRow["total"]; ok {
			switch v := t.(type) {
			case int64:
				total = int(v)
			case float64:
				total = int(v)
			}
		}
	}

	tracks := make([]map[string]any, len(rows))
	for i, r := range rows {
		tracks[i] = map[string]any{
			"trackid":           r["trackid"],
			"measurement_count": r["measurement_count"],
			"first_measurement": r["first_measurement"],
			"last_measurement":  r["last_measurement"],
			"bbox": map[string]any{
				"min_lat": r["min_lat"],
				"max_lat": r["max_lat"],
				"min_lon": r["min_lon"],
				"max_lon": r["max_lon"],
			},
			"avg_doserate": r["avg_doserate"],
		}
	}

	result := map[string]any{
		"count":           len(tracks),
		"total_available": total,
		"source":          "database",
		"filters": map[string]any{
			"year":  nilIfZero(year),
			"month": nilIfZero(month),
		},
		"tracks": tracks,
	}

	return jsonResult(result)
}

func listTracksAPI(ctx context.Context, year, month, limit int) (*mcp.CallToolResult, error) {
	allImports, err := client.GetBGeigieImports(ctx, 1000)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	filtered := allImports
	if year != 0 {
		var dateFiltered []map[string]any
		for _, imp := range allImports {
			createdAt, ok := imp["created_at"].(string)
			if !ok || createdAt == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				t, err = time.Parse("2006-01-02T15:04:05.000Z", createdAt)
				if err != nil {
					continue
				}
			}
			if t.Year() != year {
				continue
			}
			if month != 0 && int(t.Month()) != month {
				continue
			}
			dateFiltered = append(dateFiltered, imp)
		}
		filtered = dateFiltered
	}

	if limit > len(filtered) {
		limit = len(filtered)
	}
	limited := filtered[:limit]

	tracks := make([]map[string]any, len(limited))
	for i, imp := range limited {
		tracks[i] = map[string]any{
			"id":                 imp["id"],
			"name":               imp["name"],
			"description":        imp["description"],
			"cities":             imp["cities"],
			"credits":            imp["credits"],
			"measurement_count":  imp["measurements_count"],
			"status":             imp["status"],
			"approved":           imp["approved"],
			"rejected":           imp["rejected"],
			"created_at":         imp["created_at"],
			"updated_at":         imp["updated_at"],
			"subtype":            imp["subtype"],
			"would_auto_approve": imp["would_auto_approve"],
			"orientation":        imp["orientation"],
			"height":             imp["height"],
		}
	}

	result := map[string]any{
		"count":           len(tracks),
		"total_filtered":  len(filtered),
		"total_available": len(allImports),
		"source":          "api",
		"filters": map[string]any{
			"year":  nilIfZero(year),
			"month": nilIfZero(month),
		},
		"tracks": tracks,
	}

	return jsonResult(result)
}

func nilIfZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
