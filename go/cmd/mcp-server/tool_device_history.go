package main

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

var deviceHistoryToolDef = mcp.NewTool("device_history",
	mcp.WithDescription("Get historical radiation measurements from a specific monitoring device over a time period."),
	mcp.WithString("device_id",
		mcp.Description("Device identifier"),
		mcp.Required(),
	),
	mcp.WithNumber("days",
		mcp.Description("Number of days of history to retrieve (default: 30, max: 365)"),
		mcp.Min(1), mcp.Max(365),
		mcp.DefaultNumber(30),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of measurements to return (default: 200, max: 10000)"),
		mcp.Min(1), mcp.Max(10000),
		mcp.DefaultNumber(200),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleDeviceHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceIDStr, err := req.RequireString("device_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	days := req.GetInt("days", 30)
	limit := req.GetInt("limit", 200)

	if days < 1 || days > 365 {
		return mcp.NewToolResultError("days must be between 1 and 365"), nil
	}
	if limit < 1 || limit > 10000 {
		return mcp.NewToolResultError("Limit must be between 1 and 10000"), nil
	}

	if dbAvailable() {
		return deviceHistoryDB(ctx, deviceIDStr, days, limit)
	}
	return deviceHistoryAPI(ctx, deviceIDStr, days, limit)
}

func deviceHistoryDB(ctx context.Context, deviceID string, days, limit int) (*mcp.CallToolResult, error) {
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -days)

	// Try markers table first (device_id is text there)
	query := `
		SELECT id, doserate AS value, 'µSv/h' AS unit,
			to_timestamp(date) AS captured_at,
			lat AS latitude, lon AS longitude,
			altitude AS height, detector, trackid
		FROM markers
		WHERE device_id = $1 AND date >= $2 AND date <= $3
		ORDER BY date DESC
		LIMIT $4`

	rows, err := queryRows(ctx, query, deviceID, startDate.Unix(), now.Unix(), limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// If no results from markers, try realtime_measurements
	if len(rows) == 0 {
		rtQuery := `
			SELECT id, value, unit,
				to_timestamp(measured_at) AS captured_at,
				lat AS latitude, lon AS longitude,
				device_name, transport
			FROM realtime_measurements
			WHERE device_id = $1 AND measured_at >= $2 AND measured_at <= $3
			ORDER BY measured_at DESC
			LIMIT $4`

		rows, err = queryRows(ctx, rtQuery, deviceID, startDate.Unix(), now.Unix(), limit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
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
			"height":   r["height"],
			"detector": r["detector"],
		}
	}

	capturedAfter := startDate.Format("2006-01-02") + " 00:00"
	capturedBefore := now.Format("2006-01-02") + " 23:59"

	result := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"period": map[string]any{
			"days":       days,
			"start_date": capturedAfter,
			"end_date":   capturedBefore,
		},
		"count":        len(measurements),
		"source":       "database",
		"measurements": measurements,
	}

	return jsonResult(result)
}

func deviceHistoryAPI(ctx context.Context, deviceIDStr string, days, limit int) (*mcp.CallToolResult, error) {
	resp, err := client.GetRealtimeHistory(ctx, deviceIDStr)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -days)
	capturedAfter := startDate.Format("2006-01-02") + " 00:00"
	capturedBefore := now.Format("2006-01-02") + " 23:59"
	startUnix := startDate.Unix()

	// Extract dose rate time series and filter by date range
	var measurements []map[string]any
	if series, ok := resp["series"].(map[string]any); ok {
		if doseRate, ok := series["doseRate"].([]any); ok {
			for _, raw := range doseRate {
				pt, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				ts, ok := toFloat(pt["time"])
				if !ok || int64(ts) < startUnix {
					continue
				}
				t := time.Unix(int64(ts), 0).UTC()
				measurements = append(measurements, map[string]any{
					"value":       pt["value"],
					"unit":        "µSv/h",
					"captured_at": t.Format(time.RFC3339),
				})
			}
		}
	}

	totalAvailable := len(measurements)
	if limit > len(measurements) {
		limit = len(measurements)
	}
	measurements = measurements[:limit]

	deviceInfo := map[string]any{
		"id": deviceIDStr,
	}
	if name, ok := resp["deviceName"].(string); ok && name != "" {
		deviceInfo["name"] = name
	}
	if tube, ok := resp["tube"].(string); ok && tube != "" {
		deviceInfo["sensor"] = tube
	}

	result := map[string]any{
		"device": deviceInfo,
		"period": map[string]any{
			"days":       days,
			"start_date": capturedAfter,
			"end_date":   capturedBefore,
		},
		"count":           len(measurements),
		"total_available": totalAvailable,
		"source":          "api",
		"measurements":    measurements,
	}

	if ranges, ok := resp["ranges"].(map[string]any); ok {
		if dr, ok := ranges["doseRate"].(map[string]any); ok {
			result["statistics"] = map[string]any{
				"min_usvh": dr["min"],
				"max_usvh": dr["max"],
				"avg_usvh": dr["avg"],
			}
		}
	}

	return jsonResult(result)
}
