package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var sensorCurrentToolDef = mcp.NewTool("sensor_current",
	mcp.WithDescription("Get the latest reading(s) from REAL-TIME fixed sensors (Pointcast, Solarcast, bGeigieZen, etc.). Use this tool when users ask about 'current', 'latest', 'live', or 'real-time' sensor data. NOT for mobile bGeigie devices - use device_history for those. The 'unit' field indicates the measurement unit - CPM means 'counts per minute' (NOT counts per second). Always present radiation values in µSv/h by converting from CPM using detector-specific factors. IMPORTANT: Every response includes an _ai_generated_note field. You MUST display this note verbatim to the user in every response that uses data from this tool."),
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
	return mcp.NewToolResultError("Database connection required for sensor_current tool. Please ensure DATABASE_URL is set to access real-time sensor data."), nil
}

func sensorCurrentDB(ctx context.Context, deviceID string, minLat, maxLat, minLon, maxLon float64, limit int) (*mcp.CallToolResult, error) {
	// Check what tables are available in the database
	tablesQuery := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public'
		ORDER BY table_name
	`
	
	tableRows, err := queryRows(ctx, tablesQuery)
	if err != nil {
		return mcp.NewToolResultError("Could not query database schema: " + err.Error()), nil
	}
	
	// Look for tables that might contain real-time sensor data
	availableTables := make([]string, len(tableRows))
	realtimeTable := ""
	for i, row := range tableRows {
		if tableName, ok := row["table_name"].(string); ok {
			availableTables[i] = tableName
			// Check for possible real-time sensor data tables
			if tableName == "realtime_measurements" || 
			   tableName == "measurements_realtime" || 
			   tableName == "sensors" ||
			   tableName == "devices" {
				realtimeTable = tableName
			}
		}
	}
	
	if realtimeTable == "" {
		// If no real-time table found, return available tables for debugging
		result := map[string]any{
			"message": "No known real-time sensor data tables found in database.",
			"available_tables": availableTables,
			"suggestion": "Real-time sensor data may not be available through this database connection.",
		}
		return jsonResult(result)
	}
	
	var query string
	var args []interface{}

	if deviceID != "" {
		// Get latest reading from specific device
		query = fmt.Sprintf(`
			SELECT 
				id,
				device_id,
				COALESCE(device_name, device_id) AS device_name,
				value,
				COALESCE(unit, 'µSv/h') AS unit,
				to_timestamp(measured_at) AS captured_at,
				lat AS latitude,
				lon AS longitude,
				COALESCE(transport, '') AS transport
			FROM %s
			WHERE device_id = $1
			ORDER BY measured_at DESC
			LIMIT 1`, realtimeTable)
		
		args = []interface{}{deviceID}
	} else {
		// Get latest readings from all sensors in geographic area
		query = fmt.Sprintf(`
			SELECT 
				rm.id,
				rm.device_id,
				COALESCE(rm.device_name, rm.device_id) AS device_name,
				rm.value,
				COALESCE(rm.unit, 'µSv/h') AS unit,
				to_timestamp(rm.measured_at) AS captured_at,
				rm.lat AS latitude,
				rm.lon AS longitude,
				COALESCE(rm.transport, '') AS transport
			FROM %s rm
			INNER JOIN (
				SELECT device_id, MAX(measured_at) as max_measured_at
				FROM %s
				WHERE lat >= $1 AND lat <= $2 AND lon >= $3 AND lon <= $4
				GROUP BY device_id
			) latest ON rm.device_id = latest.device_id AND rm.measured_at = latest.max_measured_at
			WHERE rm.lat >= $1 AND rm.lat <= $2 AND rm.lon >= $3 AND rm.lon <= $4
			ORDER BY rm.measured_at DESC
			LIMIT $5`, realtimeTable, realtimeTable)
		
		args = []interface{}{minLat, maxLat, minLon, maxLon, limit}
	}

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error querying %s table: %v", realtimeTable, err)), nil
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
			"type": r["transport"],
		}
	}

	result := map[string]any{
		"count":    len(readings),
		"source":   "database",
		"readings": readings,
		"table_used": realtimeTable,
		"available_tables": availableTables,
		"_ai_generated_note": "This data was retrieved by an AI assistant using Safecast tools. The interpretation and presentation of this data may be influenced by the AI system.",
	}

	return jsonResult(result)
}