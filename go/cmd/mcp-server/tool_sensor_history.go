package main

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

var sensorHistoryToolDef = mcp.NewTool("sensor_history",
	mcp.WithDescription("Pull time-series data from a fixed sensor over a date range."),
	mcp.WithString("device_id",
		mcp.Description("Device identifier to get historical data from"),
		mcp.Required(),
	),
	mcp.WithString("start_date",
		mcp.Description("Start date in YYYY-MM-DD format"),
		mcp.Required(),
	),
	mcp.WithString("end_date",
		mcp.Description("End date in YYYY-MM-DD format (default: today)"),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of measurements to return (default: 200, max: 10000)"),
		mcp.Min(1), mcp.Max(10000),
		mcp.DefaultNumber(200),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleSensorHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := req.RequireString("device_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	startDateStr, err := req.RequireString("start_date")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	endDateStr := req.GetString("end_date", "")
	if endDateStr == "" {
		endDateStr = time.Now().Format("2006-01-02")
	}

	limit := req.GetInt("limit", 200)

	if limit < 1 || limit > 10000 {
		return mcp.NewToolResultError("Limit must be between 1 and 10000"), nil
	}

	// Parse dates
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return mcp.NewToolResultError("start_date must be in YYYY-MM-DD format"), nil
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return mcp.NewToolResultError("end_date must be in YYYY-MM-DD format"), nil
	}

	if endDate.Before(startDate) {
		return mcp.NewToolResultError("end_date must be after start_date"), nil
	}

	if dbAvailable() {
		return sensorHistoryDB(ctx, deviceID, startDate, endDate, limit)
	}
	
	// Fallback to API if database not available
	return mcp.NewToolResultError("Database connection required for sensor_history tool"), nil
}

func sensorHistoryDB(ctx context.Context, deviceID string, startDate, endDate time.Time, limit int) (*mcp.CallToolResult, error) {
	// Query the realtime_measurements table for time-series data
	query := `
		SELECT 
			id,
			device_id,
			device_name,
			value,
			unit,
			to_timestamp(measured_at) AS captured_at,
			lat AS latitude,
			lon AS longitude,
			transport,
			height
		FROM realtime_measurements
		WHERE device_id = $1 
			AND measured_at >= $2 
			AND measured_at <= $3
		ORDER BY measured_at ASC
		LIMIT $4`

	startUnix := startDate.Unix()
	endUnix := endDate.Unix()

	rows, err := queryRows(ctx, query, deviceID, startUnix, endUnix, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	measurements := make([]map[string]any, len(rows))
	for i, r := range rows {
		measurements[i] = map[string]any{
			"id":          r["id"],
			"device_id":   r["device_id"],
			"device_name": r["device_name"],
			"value":       r["value"],
			"unit":        r["unit"],
			"captured_at": r["captured_at"],
			"location": map[string]any{
				"latitude":  r["latitude"],
				"longitude": r["longitude"],
			},
			"type":   r["transport"],
			"height": r["height"],
		}
	}

	capturedAfter := startDate.Format("2006-01-02") + " 00:00"
	capturedBefore := endDate.Format("2006-01-02") + " 23:59"

	result := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"period": map[string]any{
			"start_date": capturedAfter,
			"end_date":   capturedBefore,
		},
		"count":        len(measurements),
		"source":       "database",
		"measurements": measurements,
	}

	return jsonResult(result)
}