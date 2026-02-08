package main

import (
	"context"
	"math"

	"github.com/mark3labs/mcp-go/mcp"
)

var searchAreaToolDef = mcp.NewTool("search_area",
	mcp.WithDescription("Find radiation measurements within a geographic bounding box."),
	mcp.WithNumber("min_lat",
		mcp.Description("Southern boundary latitude"),
		mcp.Min(-90), mcp.Max(90),
		mcp.Required(),
	),
	mcp.WithNumber("max_lat",
		mcp.Description("Northern boundary latitude"),
		mcp.Min(-90), mcp.Max(90),
		mcp.Required(),
	),
	mcp.WithNumber("min_lon",
		mcp.Description("Western boundary longitude"),
		mcp.Min(-180), mcp.Max(180),
		mcp.Required(),
	),
	mcp.WithNumber("max_lon",
		mcp.Description("Eastern boundary longitude"),
		mcp.Min(-180), mcp.Max(180),
		mcp.Required(),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of results to return (default: 100, max: 200)"),
		mcp.Min(1), mcp.Max(200),
		mcp.DefaultNumber(100),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleSearchArea(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	minLat, err := req.RequireFloat("min_lat")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxLat, err := req.RequireFloat("max_lat")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	minLon, err := req.RequireFloat("min_lon")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxLon, err := req.RequireFloat("max_lon")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", 100)

	if minLat < -90 || minLat > 90 || maxLat < -90 || maxLat > 90 {
		return mcp.NewToolResultError("Latitude must be between -90 and 90"), nil
	}
	if minLon < -180 || minLon > 180 || maxLon < -180 || maxLon > 180 {
		return mcp.NewToolResultError("Longitude must be between -180 and 180"), nil
	}
	if minLat >= maxLat {
		return mcp.NewToolResultError("min_lat must be less than max_lat"), nil
	}
	if minLon >= maxLon {
		return mcp.NewToolResultError("min_lon must be less than max_lon"), nil
	}
	if limit < 1 || limit > 200 {
		return mcp.NewToolResultError("Limit must be between 1 and 200"), nil
	}

	if dbAvailable() {
		return searchAreaDB(ctx, minLat, maxLat, minLon, maxLon, limit)
	}
	return searchAreaAPI(ctx, minLat, maxLat, minLon, maxLon, limit)
}

func searchAreaDB(ctx context.Context, minLat, maxLat, minLon, maxLon float64, limit int) (*mcp.CallToolResult, error) {
	query := `
		SELECT id, doserate AS value, 'µSv/h' AS unit,
			to_timestamp(date) AS captured_at,
			lat AS latitude, lon AS longitude,
			device_id, altitude AS height, detector,
			trackid, has_spectrum
		FROM markers
		WHERE geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)
		ORDER BY date DESC
		LIMIT $5`

	rows, err := queryRows(ctx, query, minLon, minLat, maxLon, maxLat, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	countRow, _ := queryRow(ctx, `
		SELECT count(*) AS total
		FROM markers
		WHERE geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)`,
		minLon, minLat, maxLon, maxLat)
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
			"track_id":    r["trackid"],
			"has_spectrum": r["has_spectrum"],
		}
	}

	result := map[string]any{
		"count":           len(measurements),
		"total_available": total,
		"source":          "database",
		"bbox": map[string]any{
			"min_lat": minLat,
			"max_lat": maxLat,
			"min_lon": minLon,
			"max_lon": maxLon,
		},
		"measurements": measurements,
	}

	return jsonResult(result)
}

func searchAreaAPI(ctx context.Context, minLat, maxLat, minLon, maxLon float64, limit int) (*mcp.CallToolResult, error) {
	centerLat, centerLon, radiusM := calculateCenterAndRadius(minLat, maxLat, minLon, maxLon)

	measurements, err := client.GetMeasurements(ctx, MeasurementParams{
		Latitude:  floatPtr(centerLat),
		Longitude: floatPtr(centerLon),
		Distance:  floatPtr(radiusM),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var filtered []map[string]any
	for _, m := range measurements {
		lat, latOk := toFloat(m["latitude"])
		lon, lonOk := toFloat(m["longitude"])
		if latOk && lonOk && lat >= minLat && lat <= maxLat && lon >= minLon && lon <= maxLon {
			filtered = append(filtered, m)
		}
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
		"count":         len(normalized),
		"total_in_bbox": len(filtered),
		"total_fetched": len(measurements),
		"source":        "api",
		"bbox": map[string]any{
			"min_lat": minLat,
			"max_lat": maxLat,
			"min_lon": minLon,
			"max_lon": maxLon,
		},
		"measurements": normalized,
	}

	return jsonResult(result)
}

// calculateCenterAndRadius computes the center point and search radius from a bounding box.
func calculateCenterAndRadius(minLat, maxLat, minLon, maxLon float64) (centerLat, centerLon, radiusM float64) {
	centerLat = (minLat + maxLat) / 2
	centerLon = (minLon + maxLon) / 2

	const R = 6371e3
	phi1 := minLat * math.Pi / 180
	phi2 := maxLat * math.Pi / 180
	deltaPhi := (maxLat - minLat) * math.Pi / 180
	deltaLambda := (maxLon - minLon) * math.Pi / 180

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	distance := R * c
	radiusM = math.Min(distance*0.6, 50000)
	return
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}
