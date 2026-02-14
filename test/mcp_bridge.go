package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// MCP Protocol Types
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      ServerInfo             `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MCPToolsResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	Annotations  interface{}     `json:"annotations,omitempty"`
}

// MCPBridge handles forwarding requests to the upstream MCP server
type MCPBridge struct {
	upstreamURL string
	sessions    sync.Map // Store session info: map[sessionID]map[string]interface{}
	httpClient  *http.Client
}

func NewMCPBridge(upstreamURL string) *MCPBridge {
	return &MCPBridge{
		upstreamURL: upstreamURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ServeHTTP implements the MCP server protocol
func (mb *MCPBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request: %s %s", r.Method, r.URL.Path)
	
	if r.Method != http.MethodPost {
		log.Printf("Method not allowed: %s", r.Method)
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		mb.sendError(w, nil, -32700, "Parse error: unable to read request body")
		return
	}

	log.Printf("Request body: %s", string(body))

	var req MCPRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		mb.sendError(w, nil, -32700, "Parse error: invalid JSON")
		return
	}

	// Get or create session ID from headers
	sessionID := r.Header.Get("Mcp-Session-Id")
	log.Printf("Session ID from request: %s", sessionID)
	if sessionID == "" {
		// Generate a new session ID if none provided
		sessionID = mb.generateSessionID()
		log.Printf("Generated new session ID: %s", sessionID)
		w.Header().Set("Mcp-Session-Id", sessionID)
	} else {
		log.Printf("Using existing session ID: %s", sessionID)
	}

	// Process the request based on method
	response := mb.handleRequest(sessionID, &req)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	log.Printf("Sending response: %+v", response)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func (mb *MCPBridge) handleRequest(sessionID string, req *MCPRequest) *MCPResponse {
	// Forward all requests to the upstream server
	return mb.forwardRequest(sessionID, req)
}

func (mb *MCPBridge) handleInitialize(sessionID string, req *MCPRequest) *MCPResponse {
	var params InitializeParams
	
	// Convert the params interface{} to bytes and then unmarshal
	paramsData, err := json.Marshal(req.Params)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}
	
	if err := json.Unmarshal(paramsData, &params); err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Invalid params: " + err.Error(),
			},
		}
	}

	// Store session info
	sessionData := map[string]interface{}{
		"protocolVersion": params.ProtocolVersion,
		"clientInfo":      params.ClientInfo,
	}
	mb.sessions.Store(sessionID, sessionData)

	result := InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities: map[string]interface{}{
			"tools": map[string]bool{
				"listChanged": true,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "safecast-mcp-bridge",
			Version: "1.0.0",
		},
	}

	resultBytes, _ := json.Marshal(result)
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultBytes,
	}
}

func (mb *MCPBridge) handleToolsList(sessionID string, req *MCPRequest) *MCPResponse {
	// Forward the tools/list request to the upstream server
	forwardedReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      req.ID,
		Method:  "tools/list",
		Params:  req.Params,
	}

	return mb.forwardRequestWithSession(sessionID, &forwardedReq)
}

func (mb *MCPBridge) handleToolCall(sessionID string, req *MCPRequest) *MCPResponse {
	// Forward the tools/call request to the upstream server
	forwardedReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      req.ID,
		Method:  "tools/call",
		Params:  req.Params,
	}

	return mb.forwardRequestWithSession(sessionID, &forwardedReq)
}

func (mb *MCPBridge) forwardRequest(sessionID string, req *MCPRequest) *MCPResponse {
	return mb.forwardRequestWithSession(sessionID, req)
}

func (mb *MCPBridge) forwardRequestWithSession(sessionID string, req *MCPRequest) *MCPResponse {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: "Internal error: unable to marshal request",
			},
		}
	}

	httpReq, err := http.NewRequest("POST", mb.upstreamURL, bytes.NewReader(reqBytes))
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: "Internal error: unable to create HTTP request",
			},
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	
	// Track upstream session ID separately from downstream session ID
	upstreamSessionKey := "upstream_session_" + sessionID
	if upstreamSID, ok := mb.sessions.Load(upstreamSessionKey); ok {
		// Use the stored upstream session ID
		httpReq.Header.Set("Mcp-Session-Id", upstreamSID.(string))
	} else {
		// For initialize request, we don't have upstream session yet
		// For other requests, if no upstream session exists, return error
		if req.Method != "initialize" {
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &MCPError{
					Code:    -32603,
					Message: "Upstream session not initialized",
				},
			}
		}
		// For initialize, don't set session ID in header
	}

	resp, err := mb.httpClient.Do(httpReq)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: fmt.Sprintf("Upstream error: %v", err),
			},
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: "Upstream error: unable to read response",
			},
		}
	}

	// Check if this is an initialize response and store the upstream session ID
	if req.Method == "initialize" {
		upstreamSID := resp.Header.Get("Mcp-Session-Id")
		if upstreamSID != "" {
			// Store the upstream session ID associated with the downstream session
			upstreamSessionKey := "upstream_session_" + sessionID
			mb.sessions.Store(upstreamSessionKey, upstreamSID)
		}
	}

	// Debug: Print the raw response from upstream
	log.Printf("Upstream response: %s", string(respBody))

	// Just pass through the upstream response directly without parsing
	// This preserves the exact format that the upstream server sends
	var tempResp map[string]interface{}
	if err := json.Unmarshal(respBody, &tempResp); err != nil {
		log.Printf("Error parsing upstream response: %v", err)
		log.Printf("Raw response: %s", string(respBody))
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: "Upstream error: invalid response format: " + err.Error(),
			},
		}
	}

	// Extract result or error from the parsed response
	var result json.RawMessage
	var responseError *MCPError

	if resultVal, exists := tempResp["result"]; exists {
		if resultBytes, err := json.Marshal(resultVal); err == nil {
			result = resultBytes
		}
	} else if errorVal, exists := tempResp["error"]; exists {
		if errorBytes, err := json.Marshal(errorVal); err == nil {
			var mcpe MCPError
			json.Unmarshal(errorBytes, &mcpe)
			responseError = &mcpe
		}
	}

	// Return the upstream response with our session ID
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID, // Use original request ID
		Result:  result,
		Error:   responseError,
	}
}

func (mb *MCPBridge) sendError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (mb *MCPBridge) generateSessionID() string {
	return fmt.Sprintf("mcp-session-%d", time.Now().UnixNano())
}

func main() {
	upstreamURL := os.Getenv("SAFECAST_MCP_URL")
	if upstreamURL == "" {
		upstreamURL = "https://vps-01.safecast.jp/mcp-http"
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8081" // Using a different port than the Qwen bridge
	}

	bridge := NewMCPBridge(upstreamURL)

	log.Printf("MCP Bridge starting on %s, forwarding to %s", listenAddr, upstreamURL)
	log.Fatal(http.ListenAndServe(listenAddr, bridge))
}