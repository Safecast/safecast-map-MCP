package main

import (
	"context"
	"log"
	"net/http"
	"os"

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

	// Health check tool
	mcpServer.AddTool(
		mcp.Tool{
			Name:        "ping",
			Description: "Health check tool",
		},
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	)

	// Safecast tools
	mcpServer.AddTool(queryRadiationToolDef, handleQueryRadiation)
	mcpServer.AddTool(searchAreaToolDef, handleSearchArea)
	mcpServer.AddTool(listTracksToolDef, handleListTracks)
	mcpServer.AddTool(getTrackToolDef, handleGetTrack)
	mcpServer.AddTool(deviceHistoryToolDef, handleDeviceHistory)
	mcpServer.AddTool(getSpectrumToolDef, handleGetSpectrum)
	mcpServer.AddTool(radiationInfoToolDef, handleRadiationInfo)

	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3333"
	}

	// SSE transport (legacy, for existing clients)
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
	mux.Handle("/", sseServer) // SSE server matches /mcp/sse and /mcp/message internally

	log.Println("Starting MCP server on :3333")
	log.Println("  SSE endpoint: /mcp/sse")
	log.Println("  Streamable HTTP endpoint: /mcp-http")
	if err := http.ListenAndServe(":3333", mux); err != nil {
		log.Fatal(err)
	}
}
