package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var queryRadiationToolDef = mcp.NewTool("query_radiation",
	mcp.WithDescription("Find radiation measurements near a geographic location. Returns measurements within a specified radius of the given coordinates."),
	mcp.WithNumber("lat",
		mcp.Description("Latitude (-90 to 90)"),
		mcp.Min(-90), mcp.Max(90),
		mcp.Required(),
	),
	mcp.WithNumber("lon",
		mcp.Description("Longitude (-180 to 180)"),
		mcp.Min(-180), mcp.Max(180),
		mcp.Required(),
	),
	mcp.WithNumber("radius_m",
		mcp.Description("Search radius in meters (default: 1500, max: 50000)"),
		mcp.Min(25), mcp.Max(50000),
		mcp.DefaultNumber(1500),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of results to return (default: 25, max: 10000)"),
		mcp.Min(1), mcp.Max(10000),
		mcp.DefaultNumber(25),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleQueryRadiation(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	lat, err := req.RequireFloat("lat")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	lon, err := req.RequireFloat("lon")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	radiusM := req.GetFloat("radius_m", 1500)
	limit := req.GetInt("limit", 25)

	if lat < -90 || lat > 90 {
		return mcp.NewToolResultError("Latitude must be between -90 and 90"), nil
	}
	if lon < -180 || lon > 180 {
		return mcp.NewToolResultError("Longitude must be between -180 and 180"), nil
	}
	if radiusM < 25 || radiusM > 50000 {
		return mcp.NewToolResultError("Radius must be between 25 and 50000 meters"), nil
	}
	if limit < 1 || limit > 10000 {
		return mcp.NewToolResultError("Limit must be between 1 and 10000"), nil
	}

	if dbAvailable() {
		return queryRadiationDB(ctx, lat, lon, radiusM, limit)
	}
	return queryRadiationAPI(ctx, lat, lon, radiusM, limit)
}

func queryRadiationDB(ctx context.Context, lat, lon, radiusM float64, limit int) (*mcp.CallToolResult, error) {
	query := `
		SELECT id, doserate AS value, 'µSv/h' AS unit,
			to_timestamp(date) AS captured_at,
			lat AS latitude, lon AS longitude,
			device_id, altitude AS height, detector,
			trackid, has_spectrum,
			ST_Distance(geom, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography) AS distance_m
		FROM markers
		WHERE ST_DWithin(geom, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)
		ORDER BY date DESC
		LIMIT $4`

	rows, err := queryRows(ctx, query, lat, lon, radiusM, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Get total count
	countRow, _ := queryRow(ctx, `
		SELECT count(*) AS total
		FROM markers
		WHERE ST_DWithin(geom, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)`,
		lat, lon, radiusM)
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
			"device_id":    r["device_id"],
			"height":       r["height"],
			"detector":     r["detector"],
			"track_id":     r["trackid"],
			"has_spectrum":  r["has_spectrum"],
			"distance_m":   r["distance_m"],
		}
	}

	result := map[string]any{
		"count":           len(measurements),
		"total_available": total,
		"source":          "database",
		"query": map[string]any{
			"lat":      lat,
			"lon":      lon,
			"radius_m": radiusM,
		},
		"measurements": measurements,
	}

	return jsonResult(result)
}

func queryRadiationAPI(ctx context.Context, lat, lon, radiusM float64, limit int) (*mcp.CallToolResult, error) {
	resp, err := client.GetLatestNearby(ctx, lat, lon, radiusM, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	markers, _ := resp["markers"].([]any)
	normalized := make([]map[string]any, 0, len(markers))
	for _, raw := range markers {
		if m, ok := raw.(map[string]any); ok {
			normalized = append(normalized, normalizeLatestMarker(m))
		}
	}

	result := map[string]any{
		"count":  len(normalized),
		"source": "api",
		"query": map[string]any{
			"lat":      lat,
			"lon":      lon,
			"radius_m": radiusM,
		},
		"measurements": normalized,
	}

	return jsonResult(result)
}
