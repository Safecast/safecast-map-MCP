package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var sensorCurrentToolDef = mcp.NewTool("sensor_current",
	mcp.WithDescription("Get the latest reading(s) from a specific sensor or from all sensors in a geographic area."),
	mcp.WithString("device_id",
		mcp.Description("Specific device ID to get latest reading from"),
	),
	mcp.WithNumber("min_lat",
		mcp.Description("Southern boundary for geographic filter"),
		mcp.Min(-90), mcp.Max(90),
	),
	mcp.WithNumber("max_lat",
		mcp.Description("Northern boundary for geographic filter"),
		mcp.Min(-90), mcp.Max(90),
	),
	mcp.WithNumber("min_lon",
		mcp.Description("Western boundary for geographic filter"),
		mcp.Min(-180), mcp.Max(180),
	),
	mcp.WithNumber("max_lon",
		mcp.Description("Eastern boundary for geographic filter"),
		mcp.Min(-180), mcp.Max(180),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of readings to return (default: 25, max: 1000)"),
		mcp.Min(1), mcp.Max(1000),
		mcp.DefaultNumber(25),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleSensorCurrent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID := req.GetString("device_id", "")
	minLat := req.GetFloat("min_lat", -90)
	maxLat := req.GetFloat("max_lat", 90)
	minLon := req.GetFloat("min_lon", -180)
	maxLon := req.GetFloat("max_lon", 180)
	limit := req.GetInt("limit", 25)

	if limit < 1 || limit > 1000 {
		return mcp.NewToolResultError("Limit must be between 1 and 1000"), nil
	}

	if dbAvailable() {
		return sensorCurrentDB(ctx, deviceID, minLat, maxLat, minLon, maxLon, limit)
	}
	
	// Fallback to API if database not available
	return mcp.NewToolResultError("Database connection required for sensor_current tool"), nil
}

func sensorCurrentDB(ctx context.Context, deviceID string, minLat, maxLat, minLon, maxLon float64, limit int) (*mcp.CallToolResult, error) {
	var query string
	var args []interface{}

	if deviceID != "" {
		// Get latest reading from specific device
		query = `
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
			ORDER BY measured_at DESC
			LIMIT 1`
		
		args = []interface{}{deviceID}
	} else {
		// Get latest readings from all sensors in geographic area
		query = `
			SELECT 
				rm.id,
				rm.device_id,
				rm.device_name,
				rm.value,
				rm.unit,
				to_timestamp(rm.measured_at) AS captured_at,
				rm.lat AS latitude,
				rm.lon AS longitude,
				rm.transport,
				rm.height
			FROM realtime_measurements rm
			INNER JOIN (
				SELECT device_id, MAX(measured_at) as max_measured_at
				FROM realtime_measurements
				WHERE lat >= $1 AND lat <= $2 AND lon >= $3 AND lon <= $4
				GROUP BY device_id
			) latest ON rm.device_id = latest.device_id AND rm.measured_at = latest.max_measured_at
			WHERE rm.lat >= $1 AND rm.lat <= $2 AND rm.lon >= $3 AND rm.lon <= $4
			ORDER BY rm.measured_at DESC
			LIMIT $5`
		
		args = []interface{}{minLat, maxLat, minLon, maxLon, limit}
	}

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	readings := make([]map[string]any, len(rows))
	for i, r := range rows {
		readings[i] = map[string]any{
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

	result := map[string]any{
		"count":    len(readings),
		"source":   "database",
		"readings": readings,
	}

	return jsonResult(result)
}