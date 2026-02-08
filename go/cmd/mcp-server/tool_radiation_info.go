package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

var validTopics = []string{"units", "dose_rates", "safety_levels", "detectors", "background_levels", "isotopes"}

var radiationInfoToolDef = mcp.NewTool("radiation_info",
	mcp.WithDescription("Get educational reference information about radiation units, safety levels, detectors, and related topics. Returns static reference content."),
	mcp.WithString("topic",
		mcp.Description("Topic to retrieve information about"),
		mcp.Enum(validTopics...),
		mcp.Required(),
	),
	mcp.WithReadOnlyHintAnnotation(true),
)

func handleRadiationInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	topic, err := req.RequireString("topic")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	normalized := strings.ToLower(strings.ReplaceAll(topic, "-", "_"))

	content, ok := referenceData[normalized]
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf(
			"Invalid topic: %q. Valid topics: %s", topic, strings.Join(validTopics, ", "),
		)), nil
	}

	result := map[string]any{
		"topic":   normalized,
		"content": content,
	}

	return jsonResult(result)
}
