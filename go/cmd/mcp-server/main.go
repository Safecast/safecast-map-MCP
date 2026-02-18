package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"safecast-mcp",
		"1.0.0",
	)

	// Initialize database connection (optional — falls back to REST API)
	if os.Getenv("DATABASE_URL") != "" {
		if err := initDB(); err != nil {
			log.Printf("Warning: database connection failed: %v (using REST API fallback)", err)
		} else {
			log.Println("Connected to PostgreSQL database")
		}
	} else {
		log.Println("No DATABASE_URL set, using REST API only")
	}

	// Initialize DuckDB Analytics
	if err := initDuckDB(); err != nil {
		log.Printf("Warning: failed to initialize DuckDB: %v (analytics features disabled)", err)
	} else {
		log.Println("Initialized DuckDB analytics engine")
	}

	// Health check tool
	mcpServer.AddTool(
		mcp.NewTool("ping",
			mcp.WithDescription("Health check tool"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	)

	// Safecast tools (Instrumented)
	mcpServer.AddTool(queryRadiationToolDef, instrument("query_radiation", handleQueryRadiation))
	mcpServer.AddTool(searchAreaToolDef, instrument("search_area", handleSearchArea))
	mcpServer.AddTool(listTracksToolDef, instrument("list_tracks", handleListTracks))
	mcpServer.AddTool(getTrackToolDef, instrument("get_track", handleGetTrack))
	mcpServer.AddTool(deviceHistoryToolDef, instrument("device_history", handleDeviceHistory))
	mcpServer.AddTool(getSpectrumToolDef, instrument("get_spectrum", handleGetSpectrum))
	mcpServer.AddTool(listSpectraToolDef, instrument("list_spectra", handleListSpectra))
	mcpServer.AddTool(radiationInfoToolDef, instrument("radiation_info", handleRadiationInfo))
	mcpServer.AddTool(dbInfoToolDef, instrument("db_info", handleDBInfo))

	// Real-time sensor tools
	mcpServer.AddTool(listSensorsToolDef, instrument("list_sensors", handleListSensors))
	mcpServer.AddTool(sensorCurrentToolDef, instrument("sensor_current", handleSensorCurrent))
	mcpServer.AddTool(sensorHistoryToolDef, instrument("sensor_history", handleSensorHistory))

    // Analytics Tools
    mcpServer.AddTool(queryAnalyticsToolDef, handleQueryAnalytics)
    mcpServer.AddTool(radiationStatsToolDef, handleRadiationStats)

	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3333"
	}

	// SSE transport (legacy, for existing clients)
	// Note: baseURL should NOT include /mcp — WithStaticBasePath adds it
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(baseURL),
		server.WithStaticBasePath("/mcp"),
	)

	// Streamable HTTP transport (for Claude.ai and modern clients)
	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath("/mcp-http"),
	)

	// Serve both on the same port
	mux := http.NewServeMux()
	mux.Handle("/mcp-http", httpServer)
	mux.Handle("/mcp/", sseServer) // SSE server handles /mcp/sse and /mcp/message

	// REST API + Swagger UI
	restHandler := &RESTHandler{}
	restHandler.Register(mux)

	// Determine listening port (default 3333)
	port := os.Getenv("MCP_PORT")
	if port == "" {
		port = "3333"
	}
	listenAddr := ":" + port
	log.Printf("Starting MCP server on %s", listenAddr)
	log.Println("  SSE endpoint: /mcp/sse")
	log.Println("  Streamable HTTP endpoint: /mcp-http")
	log.Println("  REST API: /api/...")
	log.Println("  Swagger UI: /docs/")
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}

}

// instrument wraps a tool handler with logging.
func instrument(name string, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        start := time.Now()
        res, err := h(ctx, req)
        duration := time.Since(start)
        
        // Log asynchronously
        resultCount := 0
        if res != nil {
            resultCount = len(res.Content)
        }
        
        // Convert arguments to map[string]any with type assertion
        args := make(map[string]any)
        if req.Params.Arguments != nil {
            if argsMap, ok := req.Params.Arguments.(map[string]any); ok {
                args = argsMap
            }
        }
        
        // We do this in a goroutine to not block the response
        LogQueryAsync(name, args, resultCount, duration, "claude-client") // simplistic client info
        
        return res, err
    }
}
