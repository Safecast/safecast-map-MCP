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
		mcp.Description("Maximum number of results to return (default: 50, max: 50000)"),
		mcp.Min(1), mcp.Max(50000),
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
	if limit < 1 || limit > 50000 {
		return mcp.NewToolResultError("Limit must be between 1 and 50000"), nil
	}

	if dbAvailable() {
		return listTracksDB(ctx, year, month, limit)
	}
	return listTracksAPI(ctx, year, month, limit)
}

func listTracksDB(ctx context.Context, year, month, limit int) (*mcp.CallToolResult, error) {
	query := `SELECT id, filename, file_type, track_id, file_size,
			created_at, source, source_id, recording_date,
			detector, username
		FROM uploads
		WHERE 1=1`

	args := []any{}
	argIdx := 1

	if year != 0 {
		startDate := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
		if month != 0 {
			startDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			if month == 12 {
				endDate = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
			} else {
				endDate = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
			}
		}
		query += fmt.Sprintf(" AND recording_date >= $%d AND recording_date < $%d", argIdx, argIdx+1)
		args = append(args, startDate, endDate)
		argIdx += 2
	}

	query += " ORDER BY recording_date DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Get total count (with same filters)
	countQuery := `SELECT count(*) AS total FROM uploads WHERE 1=1`
	countArgs := []any{}
	if year != 0 {
		startDate := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
		if month != 0 {
			startDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			if month == 12 {
				endDate = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
			} else {
				endDate = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
			}
		}
		countQuery += " AND recording_date >= $1 AND recording_date < $2"
		countArgs = append(countArgs, startDate, endDate)
	}
	countRow, _ := queryRow(ctx, countQuery, countArgs...)
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
			"id":             r["id"],
			"filename":       r["filename"],
			"track_id":       r["track_id"],
			"detector":       r["detector"],
			"username":       r["username"],
			"file_size":      r["file_size"],
			"recording_date": r["recording_date"],
			"created_at":     r["created_at"],
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
	var resp map[string]any
	var err error

	if year != 0 && month != 0 {
		resp, err = client.GetTracksByMonth(ctx, year, month)
	} else if year != 0 {
		resp, err = client.GetTracksByYear(ctx, year)
	} else {
		resp, err = client.GetTracks(ctx)
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	allTracks, _ := resp["tracks"].([]any)
	totalAvailable := len(allTracks)

	if limit > len(allTracks) {
		limit = len(allTracks)
	}
	limited := allTracks[:limit]

	tracks := make([]map[string]any, 0, len(limited))
	for _, raw := range limited {
		t, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tracks = append(tracks, map[string]any{
			"track_id":     t["trackID"],
			"marker_count": t["markerCount"],
			"first_id":     t["firstID"],
			"last_id":      t["lastID"],
			"index":        t["index"],
			"api_url":      t["apiURL"],
		})
	}

	result := map[string]any{
		"count":           len(tracks),
		"total_available": totalAvailable,
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
