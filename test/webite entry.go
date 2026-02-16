package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================
// Qwen API types (OpenAI-compatible)
// ============================================================

type ChatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message Message `json:"message"`
}

// ============================================================
// MCP types
// ============================================================

type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type MCPToolsResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type MCPCallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type MCPToolResult struct {
	Content []MCPContent `json:"content"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ============================================================
// MCP Client (Streamable HTTP transport)
// ============================================================

type MCPClient struct {
	ServerURL  string
	HTTPClient *http.Client
	SessionID  string
	reqID      int
}

func NewMCPClient(serverURL string) *MCPClient {
	return &MCPClient{
		ServerURL: serverURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *MCPClient) nextID() int {
	c.reqID++
	return c.reqID
}

// sendRequest sends a JSON-RPC request to the MCP server and returns the result.
// Handles both direct JSON responses and SSE-wrapped responses.
func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	rpcReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.SessionID)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Capture session ID
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.SessionID = sid
	}

	ct := resp.Header.Get("Content-Type")

	// Handle SSE response
	if strings.Contains(ct, "text/event-stream") {
		return c.readSSEResponse(resp.Body, rpcReq.ID)
	}

	// Handle direct JSON response
	var rpcResp MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return rpcResp.Result, nil
}

func (c *MCPClient) readSSEResponse(r io.Reader, expectedID int) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}

		// Empty line = end of SSE event
		if line == "" && len(dataLines) > 0 {
			data := strings.Join(dataLines, "\n")
			dataLines = nil

			var rpcResp MCPResponse
			if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
				continue // skip non-JSON events
			}
			if rpcResp.ID == expectedID {
				return rpcResp.Result, nil
			}
		}
	}
	return nil, fmt.Errorf("no matching response found in SSE stream")
}

func (c *MCPClient) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "go-mcp-qwen",
			"version": "1.0.0",
		},
	}
	_, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification (fire and forget)
	notif := MCPRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	body, _ := json.Marshal(notif)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.ServerURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.SessionID)
	}
	c.HTTPClient.Do(req)

	return nil
}

func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	result, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var toolsResult MCPToolsResult
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		return nil, fmt.Errorf("unmarshal tools: %w", err)
	}
	return toolsResult.Tools, nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	params := MCPCallToolParams{
		Name:      name,
		Arguments: args,
	}
	result, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	var toolResult MCPToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return "", fmt.Errorf("unmarshal tool result: %w", err)
	}

	var texts []string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// ============================================================
// Qwen Client
// ============================================================

type QwenClient struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func NewQwenClient(apiKey, model string) *QwenClient {
	return &QwenClient{
		APIKey:  apiKey,
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (q *QwenClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	chatReq := ChatRequest{
		Model:    q.Model,
		Messages: messages,
		Tools:    tools,
	}
	if len(tools) > 0 {
		chatReq.ToolChoice = "auto"
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+q.APIKey)

	resp, err := q.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qwen API error %d: %s", resp.StatusCode, string(b))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}
	return &chatResp.Choices[0].Message, nil
}

// ============================================================
// Bridge: convert MCP tools → Qwen tools
// ============================================================

func mcpToolsToQwenTools(mcpTools []MCPTool) []Tool {
	tools := make([]Tool, len(mcpTools))
	for i, t := range mcpTools {
		tools[i] = Tool{
			Type: "function",
			Function: Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return tools
}

// ============================================================
// Agent: ties Qwen + MCP together with a tool-call loop
// ============================================================

type Agent struct {
	qwen      *QwenClient
	mcp       *MCPClient
	tools     []Tool
	maxRounds int
}

func NewAgent(qwen *QwenClient, mcp *MCPClient) *Agent {
	return &Agent{
		qwen:      qwen,
		mcp:       mcp,
		maxRounds: 5, // max tool-call round-trips
	}
}

func (a *Agent) Init(ctx context.Context) error {
	if err := a.mcp.Initialize(ctx); err != nil {
		return fmt.Errorf("MCP init: %w", err)
	}
	mcpTools, err := a.mcp.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	a.tools = mcpToolsToQwenTools(mcpTools)
	log.Printf("Loaded %d tools from MCP server", len(a.tools))
	for _, t := range a.tools {
		log.Printf("  - %s: %s", t.Function.Name, t.Function.Description)
	}
	return nil
}

func (a *Agent) Run(ctx context.Context, userMessage string) (string, error) {
	systemPrompt := `You are a helpful assistant with access to Safecast radiation measurement tools. Follow these tool selection guidelines:

REAL-TIME DATA TOOLS (use for live/current sensor data):
- sensor_current: Get the latest reading from real-time fixed sensors (Pointcast, Solarcast, etc.)
- sensor_history: Get time-series data from real-time fixed sensors over a date range

HISTORICAL DATA TOOLS (use for mobile survey/bGeigie data):
- device_history: Get historical measurements from mobile devices (bGeigie imports)
- query_radiation: Search for measurements by location
- search_area: Find measurements in a geographic bounding box
- list_tracks, get_track: Browse mobile survey drives

IMPORTANT: When the user asks about "real-time", "current", "latest", "live", or mentions fixed sensor types (Pointcast, Solarcast, bGeigieZen), you MUST use sensor_current or sensor_history, NOT device_history.

Only use device_history for mobile bGeigie devices or when explicitly asked for historical survey data.

UNIT CONVERSION REQUIREMENT:
Always present radiation measurements in µSv/h (microsieverts per hour).
If data is provided in CPM (counts per minute), you MUST state it as "CPM (counts per minute)" NOT "CPS (counts per second)".
Convert CPM to µSv/h using these detector-specific conversion factors:

Common Geiger-Müller tube conversion factors (CPM to µSv/h):
- LND 7317 (Pancake tube): µSv/h = CPM / 334
- SBM-20 (Russian tube): µSv/h = CPM / 175.43
- SBM-19: µSv/h = CPM / 108.3
- J305 (bGeigie standard): µSv/h = CPM / 100
- LND 78017: µSv/h = CPM / 294
- SI-22G: µSv/h = CPM / 108
- SI-3BG: µSv/h = CPM / 631

If the detector type is known, use its specific conversion factor. If unknown, note that the value is in CPM and conversion requires knowing the detector model.`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	for round := 0; round < a.maxRounds; round++ {
		resp, err := a.qwen.Chat(ctx, messages, a.tools)
		if err != nil {
			return "", fmt.Errorf("qwen chat round %d: %w", round, err)
		}

		// No tool calls → we have the final answer
		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}

		// Append assistant message with tool calls
		messages = append(messages, *resp)

		// Execute each tool call against MCP
		for _, tc := range resp.ToolCalls {
			log.Printf("Tool call: %s(%s)", tc.Function.Name, tc.Function.Arguments)

			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{}
			}

			result, err := a.mcp.CallTool(ctx, tc.Function.Name, args)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			log.Printf("Tool result: %.200s...", result)

			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	return "", fmt.Errorf("exceeded max tool-call rounds (%d)", a.maxRounds)
}

// ============================================================
// HTTP handler for your website
// ============================================================

func main() {
	// Test the API key first
	testAPIKey()
	
	mcpURL := envOrDefault("MCP_SERVER_URL", "https://vps-01.safecast.jp/mcp-http")
	
	// Check for ALIBABA_CLOUD_API_KEY first, then fall back to DASHSCOPE_API_KEY
	qwenKey := os.Getenv("ALIBABA_CLOUD_API_KEY")
	if qwenKey == "" {
		qwenKey = os.Getenv("DASHSCOPE_API_KEY")
	}
	
	qwenModel := envOrDefault("QWEN_MODEL", "qwen-plus")
	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")

	if qwenKey == "" {
		log.Fatal("ALIBABA_CLOUD_API_KEY or DASHSCOPE_API_KEY environment variable required")
	}

	mcpClient := NewMCPClient(mcpURL)
	qwenClient := NewQwenClient(qwenKey, qwenModel)
	agent := NewAgent(qwenClient, mcpClient)

	ctx := context.Background()
	if err := agent.Init(ctx); err != nil {
		log.Fatalf("Agent init failed: %v", err)
	}

	// API endpoint for your website
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		answer, err := agent.Run(r.Context(), req.Message)
		if err != nil {
			log.Printf("Agent error: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": answer})
	})

	// Simple HTML page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	log.Printf("Server starting on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Test API key function
func testAPIKey() {
	apiKey := os.Getenv("ALIBABA_CLOUD_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("DASHSCOPE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("ALIBABA_CLOUD_API_KEY or DASHSCOPE_API_KEY environment variable not set")
	}

	// Create a simple request to test the API key using the same endpoint as the Qwen client
	client := &http.Client{Timeout: 30 * time.Second}

	// Create a minimal chat completion request to test the API key
	requestBody := `{"model":"qwen-turbo","messages":[{"role":"user","content":"test"}]}`
	
	req, err := http.NewRequest("POST", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", strings.NewReader(requestBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making request: %v", err)
		return
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return
	}
	
	if resp.StatusCode == 200 {
		log.Println("✓ API key is valid! Successfully connected to Alibaba Cloud.")
	} else {
		log.Printf("✗ API key test failed. Status: %d, Response: %s", resp.StatusCode, string(body))
	}
}

const indexHTML = `<!DOCTYPE html>
<html>
<head>
  <title>MCP Chat</title>
  <style>
    body { font-family: sans-serif; max-width: 700px; margin: 40px auto; padding: 0 20px; }
    #chat { border: 1px solid #ccc; padding: 16px; min-height: 300px; margin-bottom: 12px;
            overflow-y: auto; max-height: 500px; white-space: pre-wrap; }
    #input { width: calc(100% - 80px); padding: 8px; }
    button { padding: 8px 16px; }
    .user { color: #0066cc; }
    .assistant { color: #333; }
    .loading { color: #999; font-style: italic; }
  </style>
</head>
<body>
  <h2>Qwen + MCP Chat</h2>
  <div id="chat"></div>
  <input id="input" placeholder="Ask something..." onkeydown="if(event.key==='Enter')send()">
  <button onclick="send()">Send</button>
  <script>
    const chat = document.getElementById('chat');
    const input = document.getElementById('input');
    async function send() {
      const msg = input.value.trim();
      if (!msg) return;
      input.value = '';
      chat.innerHTML += '<div class="user"><b>You:</b> ' + msg + '</div>';
      chat.innerHTML += '<div class="loading">Thinking...</div>';
      chat.scrollTop = chat.scrollHeight;
      try {
        const res = await fetch('/api/chat', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({message: msg})
        });
        const data = await res.json();
        chat.querySelector('.loading')?.remove();
        chat.innerHTML += '<div class="assistant"><b>Assistant:</b> ' + data.response + '</div>';
      } catch(e) {
        chat.querySelector('.loading')?.remove();
        chat.innerHTML += '<div style="color:red">Error: ' + e.message + '</div>';
      }
      chat.scrollTop = chat.scrollHeight;
    }
  </script>
</body>
</html>`