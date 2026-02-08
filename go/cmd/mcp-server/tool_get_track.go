package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
)

var getTrackToolDef = mcp.NewTool("get_track",
	mcp.WithDescription("Retrieve all radiation measurements recorded during a specific track/journey. Use list_tracks to find available track IDs first."),
	mcp.WithString("track_id",
		mcp.Description("Track identifier (bGeigie import ID or track ID)"),
		mcp.Required(),
	),
	mcp.WithNumber("from",
		mcp.Description("Optional: Start marker ID for filtering"),
	),
	mcp.WithNumber("to",
		mcp.Description("Optional: End marker ID for filtering"),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of measurements to return (default: 200, max: 200)"),
		mcp.Min(1), mcp.Max(200),
		mcp.DefaultNumber(200),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleGetTrack(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	trackIDStr, err := req.RequireString("track_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 200)
	if limit < 1 || limit > 200 {
		return mcp.NewToolResultError("Limit must be between 1 and 200"), nil
	}

	fromID := req.GetInt("from", 0)
	toID := req.GetInt("to", 0)

	if dbAvailable() {
		return getTrackDB(ctx, trackIDStr, fromID, toID, limit)
	}
	return getTrackAPI(ctx, trackIDStr, fromID, toID, limit)
}

func getTrackDB(ctx context.Context, trackID string, fromID, toID, limit int) (*mcp.CallToolResult, error) {
	query := `
		SELECT id, doserate AS value, 'µSv/h' AS unit,
			to_timestamp(date) AS captured_at,
			lat AS latitude, lon AS longitude,
			device_id, altitude AS height, detector,
			has_spectrum
		FROM markers
		WHERE trackid = $1`

	args := []any{trackID}
	argIdx := 2

	if fromID != 0 {
		query += fmt.Sprintf(" AND id >= $%d", argIdx)
		args = append(args, fromID)
		argIdx++
	}
	if toID != 0 {
		query += fmt.Sprintf(" AND id <= $%d", argIdx)
		args = append(args, toID)
		argIdx++
	}

	query += " ORDER BY date ASC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Get total count for this track
	countRow, _ := queryRow(ctx, `SELECT count(*) AS total FROM markers WHERE trackid = $1`, trackID)
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

	measurements := make([]map[string]any, len(rows))
	for i, r := range rows {
		measurements[i] = map[string]any{
			"id":    r["id"],
			"value": r["value"],
			"unit":  r["unit"],
			"captured_at": r["captured_at"],
			"location": map[string]any{
				"latitude":  r["latitude"],
				"longitude": r["longitude"],
			},
			"device_id":   r["device_id"],
			"height":      r["height"],
			"detector":    r["detector"],
			"has_spectrum": r["has_spectrum"],
		}
	}

	result := map[string]any{
		"track_id":        trackID,
		"count":           len(measurements),
		"total_available": total,
		"source":          "database",
		"from_marker":     nilIfZero(fromID),
		"to_marker":       nilIfZero(toID),
		"measurements":    measurements,
	}

	return jsonResult(result)
}

func getTrackAPI(ctx context.Context, trackIDStr string, fromID, toID, limit int) (*mcp.CallToolResult, error) {
	trackID, err := strconv.Atoi(trackIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("track_id must be a number: %s", trackIDStr)), nil
	}

	trackInfo, err := client.GetBGeigieImport(ctx, trackID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	measurements, err := client.GetMeasurements(ctx, MeasurementParams{
		BGeigieImportID: intPtr(trackID),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	filtered := measurements
	if fromID != 0 || toID != 0 {
		var rangeFiltered []map[string]any
		for _, m := range measurements {
			id, ok := toFloat(m["id"])
			if !ok {
				continue
			}
			mid := int(id)
			if fromID != 0 && mid < fromID {
				continue
			}
			if toID != 0 && mid > toID {
				continue
			}
			rangeFiltered = append(rangeFiltered, m)
		}
		filtered = rangeFiltered
	}

	if limit > len(filtered) {
		limit = len(filtered)
	}
	limited := filtered[:limit]

	normalized := make([]map[string]any, len(limited))
	for i, m := range limited {
		normalized[i] = normalizeMeasurement(m)
	}

	result := map[string]any{
		"track": map[string]any{
			"id":                trackInfo["id"],
			"name":              trackInfo["name"],
			"description":       trackInfo["description"],
			"cities":            trackInfo["cities"],
			"credits":           trackInfo["credits"],
			"measurement_count": trackInfo["measurements_count"],
			"status":            trackInfo["status"],
			"approved":          trackInfo["approved"],
		},
		"count":           len(normalized),
		"total_available": len(filtered),
		"source":          "api",
		"from_marker":     nilIfZero(fromID),
		"to_marker":       nilIfZero(toID),
		"measurements":    normalized,
	}

	return jsonResult(result)
}
