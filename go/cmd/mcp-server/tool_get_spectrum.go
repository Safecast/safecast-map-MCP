package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var getSpectrumToolDef = mcp.NewTool("get_spectrum",
	mcp.WithDescription("Get gamma spectroscopy data for a specific measurement point."),
	mcp.WithNumber("marker_id",
		mcp.Description("Marker/measurement identifier"),
		mcp.Min(1),
		mcp.Required(),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleGetSpectrum(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	markerID, err := req.RequireInt("marker_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if markerID < 1 {
		return mcp.NewToolResultError("marker_id must be a positive number"), nil
	}

	if dbAvailable() {
		return getSpectrumDB(ctx, markerID)
	}
	return getSpectrumAPI(ctx, markerID)
}

func getSpectrumDB(ctx context.Context, markerID int) (*mcp.CallToolResult, error) {
	// Check if marker has spectrum data
	row, err := queryRow(ctx, `
		SELECT s.id, s.channels, s.channel_count, s.energy_min_kev, s.energy_max_kev,
			s.live_time_sec, s.real_time_sec, s.device_model, s.calibration,
			s.source_format, s.filename, s.created_at,
			m.doserate, m.lat, m.lon, to_timestamp(m.date) AS captured_at
		FROM spectra s
		JOIN markers m ON m.id = s.marker_id
		WHERE s.marker_id = $1`, markerID)

	if err != nil {
		// No spectrum data — check if marker exists
		marker, mErr := queryRow(ctx, `
			SELECT id, has_spectrum FROM markers WHERE id = $1`, markerID)
		if mErr != nil {
			return mcp.NewToolResultError("Marker not found"), nil
		}

		result := map[string]any{
			"marker_id": markerID,
			"available": false,
			"source":    "database",
			"message":   "No spectrum data available for this marker.",
		}
		if hs, ok := marker["has_spectrum"].(bool); ok && hs {
			result["message"] = "Marker is flagged as having spectrum data but no spectrum record was found."
		}
		return jsonResult(result)
	}

	result := map[string]any{
		"marker_id": markerID,
		"available": true,
		"source":    "database",
		"spectrum": map[string]any{
			"channels":       row["channels"],
			"channel_count":  row["channel_count"],
			"energy_min_kev": row["energy_min_kev"],
			"energy_max_kev": row["energy_max_kev"],
			"live_time_sec":  row["live_time_sec"],
			"real_time_sec":  row["real_time_sec"],
			"device_model":   row["device_model"],
			"calibration":    row["calibration"],
			"source_format":  row["source_format"],
			"filename":       row["filename"],
			"created_at":     row["created_at"],
		},
		"marker": map[string]any{
			"doserate":    row["doserate"],
			"latitude":    row["lat"],
			"longitude":   row["lon"],
			"captured_at": row["captured_at"],
		},
	}

	return jsonResult(result)
}

func getSpectrumAPI(ctx context.Context, markerID int) (*mcp.CallToolResult, error) {
	spectrum, err := client.GetSpectrum(ctx, markerID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"marker_id": markerID,
		"available": true,
		"source":    "api",
		"spectrum": map[string]any{
			"channels":       spectrum["channels"],
			"channel_count":  spectrum["channelCount"],
			"energy_min_kev": spectrum["energyMinKeV"],
			"energy_max_kev": spectrum["energyMaxKeV"],
			"live_time_sec":  spectrum["liveTimeSec"],
			"real_time_sec":  spectrum["realTimeSec"],
			"device_model":   spectrum["deviceModel"],
			"calibration":    spectrum["calibration"],
			"source_format":  spectrum["sourceFormat"],
			"filename":       spectrum["filename"],
		},
	}

	return jsonResult(result)
}
