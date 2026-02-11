package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var listSensorsToolDef = mcp.NewTool("list_sensors",
	mcp.WithDescription("Discover active fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.) by location or type, returning device IDs, locations, status, and last reading timestamp."),
	mcp.WithString("type",
		mcp.Description("Filter by sensor type (e.g., 'Pointcast', 'Solarcast', 'bGeigieZen', etc.)"),
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
		mcp.Description("Maximum number of sensors to return (default: 50, max: 1000)"),
		mcp.Min(1), mcp.Max(1000),
		mcp.DefaultNumber(50),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleListSensors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sensorType := req.GetString("type", "")
	minLat := req.GetFloat("min_lat", -90)
	maxLat := req.GetFloat("max_lat", 90)
	minLon := req.GetFloat("min_lon", -180)
	maxLon := req.GetFloat("max_lon", 180)
	limit := req.GetInt("limit", 50)

	if limit < 1 || limit > 1000 {
		return mcp.NewToolResultError("Limit must be between 1 and 1000"), nil
	}

	if dbAvailable() {
		return listSensorsDB(ctx, sensorType, minLat, maxLat, minLon, maxLon, limit)
	}
	
	// Fallback to API if database not available
	return mcp.NewToolResultError("Database connection required for list_sensors tool"), nil
}

func listSensorsDB(ctx context.Context, sensorType string, minLat, maxLat, minLon, maxLon float64, limit int) (*mcp.CallToolResult, error) {
	// Query the realtime_measurements table to find unique devices/sensors
	var query string
	var args []interface{}

	if sensorType != "" {
		// Filter by sensor type
		query = `
			SELECT DISTINCT 
				device_id,
				device_name,
				transport,
				lat AS latitude,
				lon AS longitude,
				MAX(to_timestamp(measured_at)) AS last_reading_at
			FROM realtime_measurements
			WHERE lat >= $1 AND lat <= $2 AND lon >= $3 AND lon <= $4
				AND (transport ILIKE $5 OR device_name ILIKE $5)
			GROUP BY device_id, device_name, transport, lat, lon
			ORDER BY last_reading_at DESC
			LIMIT $6`
		
		args = []interface{}{minLat, maxLat, minLon, maxLon, "%" + sensorType + "%", limit}
	} else {
		// No filter by type
		query = `
			SELECT DISTINCT 
				device_id,
				device_name,
				transport,
				lat AS latitude,
				lon AS longitude,
				MAX(to_timestamp(measured_at)) AS last_reading_at
			FROM realtime_measurements
			WHERE lat >= $1 AND lat <= $2 AND lon >= $3 AND lon <= $4
			GROUP BY device_id, device_name, transport, lat, lon
			ORDER BY last_reading_at DESC
			LIMIT $5`
		
		args = []interface{}{minLat, maxLat, minLon, maxLon, limit}
	}

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sensors := make([]map[string]any, len(rows))
	for i, r := range rows {
		sensors[i] = map[string]any{
			"device_id":       r["device_id"],
			"device_name":     r["device_name"],
			"type":            r["transport"],
			"location": map[string]any{
				"latitude":  r["latitude"],
				"longitude": r["longitude"],
			},
			"last_reading_at": r["last_reading_at"],
		}
	}

	result := map[string]any{
		"count":   len(sensors),
		"source":  "database",
		"sensors": sensors,
	}

	return jsonResult(result)
}