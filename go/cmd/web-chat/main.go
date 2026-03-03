package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

//go:embed index.html
var indexHTML []byte

//go:embed safecast-square-ct.png
var logoPNG []byte

const systemPrompt = `Safecast radiation monitoring assistant with REAL-TIME sensor data and historical archives.

**Tool Selection**
- Current/live data: sensor_current, list_sensors
- Time-series from fixed sensors: sensor_history
- Extreme readings with locations: query_extreme_readings
- Statistics: radiation_stats
- Historical surveys: query_radiation, search_area, list_tracks
- NEVER use query_radiation for current data (historical only)
- NEVER use radiation_stats for specific extreme location queries

**Data Types**
- Real-time: Pointcast/Solarcast/bGeigieZen (fixed stations)
- Historical: Mobile bGeigie surveys (archived routes)
- CPM → µSv/h: multiply by ~0.0069 (LND 7318)

**Radius Selection** (query_radiation, sensor_current):
Address: 500-1000m | District: 2000-5000m | Village: 5000-10000m | City: 15-25km | Metro: 30-50km
Always state radius used.

**Formatting**
- Hide "_ai_generated_note" field (internal use only)
- **CRITICAL: ALL devices/coords MUST be clickable map links:**
  * Devices: [pointcast:10042](https://simplemap.safecast.org/?lat=LAT&lon=LON&zoom=15)
  * Tracks: [track_id](https://simplemap.safecast.org/?lat=LAT&lon=LON&zoom=12)
  * Coords: [37.72°N, 140.48°E](https://simplemap.safecast.org/?lat=37.72&lon=140.48&zoom=15)
  * NEVER plain device names or "Visit: https://..." text
- Sensor/track data: ALWAYS use markdown tables (not lists)
- Table columns: Device ID, Type, Location, Reading, Timestamp
- Concise coords: "37.48°N, 140.48°E"

Be concise. Ask for clarification if location unclear.`

// ── Ollama / OpenAI-compatible API types ────────────────────────────────────

type ollamaTool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function toolCallFunction `json:"function"`
}

type ollamaMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type ollamaChoice struct {
	Message      ollamaMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type ollamaResponse struct {
	Choices []ollamaChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ── Streaming helpers (chunked HTTP / NDJSON) ──────────────────────────────

type chunk struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeChunk(w http.ResponseWriter, c chunk) {
	data, _ := json.Marshal(c)
	data = append(data, '\n')
	w.Write(data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func writeChunkBuffered(w http.ResponseWriter, c chunk, buffer *[]chunk, useBuffer bool) {
	if useBuffer {
		*buffer = append(*buffer, c)
	} else {
		writeChunk(w, c)
	}
}

func flushBuffer(w http.ResponseWriter, buffer []chunk) {
	for _, c := range buffer {
		writeChunk(w, c)
	}
}

// ── Ollama call ─────────────────────────────────────────────────────────────

func callOllama(ctx context.Context, ollamaURL, model string, messages []ollamaMessage, tools []ollamaTool) (*ollamaResponse, error) {
	reqBody := ollamaRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var or ollamaResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %.200s)", err, raw)
	}
	if or.Error != nil {
		return nil, fmt.Errorf("ollama error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return nil, fmt.Errorf("ollama returned no choices (body: %.200s)", raw)
	}
	return &or, nil
}

// ── MCP tool conversion ────────────────────────────────────────────────────

func mcpToolsToOllama(tools []mcp.Tool) []ollamaTool {
	var out []ollamaTool
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema)
		var ot ollamaTool
		ot.Type = "function"
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = json.RawMessage(schema)
		out = append(out, ot)
	}
	return out
}

// ── Chat handler ───────────────────────────────────────────────────────────

func handleChat(mcpURL, ollamaURL, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS preflight
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Detect if request comes through CloudFront
		isCloudfFront := r.Header.Get("CloudFront-Viewer-Country") != "" ||
			r.Header.Get("CloudFront-Forwarded-Proto") != "" ||
			r.Header.Get("X-Amz-Cf-Id") != ""

		log.Printf("Chat request: CloudFront=%v, Headers: CF-Country=%q, CF-Proto=%q, X-Amz-Cf-Id=%q",
			isCloudfFront,
			r.Header.Get("CloudFront-Viewer-Country"),
			r.Header.Get("CloudFront-Forwarded-Proto"),
			r.Header.Get("X-Amz-Cf-Id"))

		w.Header().Set("Content-Type", "application/x-ndjson")
		if !isCloudfFront {
			w.Header().Set("Transfer-Encoding", "chunked")
			w.Header().Set("X-Accel-Buffering", "no")
		}
		w.Header().Set("Cache-Control", "no-cache, no-store")

		var buffer []chunk

		ctx := r.Context()

		var chatReq struct {
			Message string          `json:"message"`
			History []ollamaMessage `json:"history,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil || chatReq.Message == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeChunkBuffered(w, chunk{Type: "error", Error: "invalid request: message required"}, &buffer, isCloudfFront)
			if isCloudfFront {
				flushBuffer(w, buffer)
			}
			return
		}

		// ── Connect to MCP server ──────────────────────────────────────────
		mc, err := mcpclient.NewStreamableHttpClient(mcpURL)
		if err != nil {
			writeChunkBuffered(w, chunk{Type: "error", Error: fmt.Sprintf("MCP connect: %v", err)}, &buffer, isCloudfFront)
			if isCloudfFront {
				flushBuffer(w, buffer)
			}
			return
		}
		defer mc.Close()

		if _, err := mc.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo:      mcp.Implementation{Name: "safecast-web-chat", Version: "1.0.0"},
			},
		}); err != nil {
			writeChunkBuffered(w, chunk{Type: "error", Error: fmt.Sprintf("MCP init: %v", err)}, &buffer, isCloudfFront)
			if isCloudfFront {
				flushBuffer(w, buffer)
			}
			return
		}

		toolsResult, err := mc.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			writeChunkBuffered(w, chunk{Type: "error", Error: fmt.Sprintf("list tools: %v", err)}, &buffer, isCloudfFront)
			if isCloudfFront {
				flushBuffer(w, buffer)
			}
			return
		}
		tools := mcpToolsToOllama(toolsResult.Tools)

		// ── Build message list ─────────────────────────────────────────────
		// System message first, then history, then new user message.
		messages := []ollamaMessage{
			{Role: "system", Content: systemPrompt},
		}
		messages = append(messages, chatReq.History...)
		messages = append(messages, ollamaMessage{Role: "user", Content: chatReq.Message})

		// ── Agentic loop ───────────────────────────────────────────────────
		for {
			resp, err := callOllama(ctx, ollamaURL, model, messages, tools)
			if err != nil {
				writeChunkBuffered(w, chunk{Type: "error", Error: err.Error()}, &buffer, isCloudfFront)
				if isCloudfFront {
					flushBuffer(w, buffer)
				}
				return
			}

			choice := resp.Choices[0]
			msg := choice.Message

			// Append assistant message to history
			messages = append(messages, msg)

			// Stream any text content
			if msg.Content != "" {
				writeChunkBuffered(w, chunk{Type: "text", Text: msg.Content}, &buffer, isCloudfFront)
			}

			// Stop if no tool calls
			if choice.FinishReason == "stop" || len(msg.ToolCalls) == 0 {
				break
			}

			// ── Execute tool calls via MCP ─────────────────────────────────
			for _, tc := range msg.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

				callReq := mcp.CallToolRequest{}
				callReq.Params.Name = tc.Function.Name
				callReq.Params.Arguments = args

				var resultText string
				toolResult, err := mc.CallTool(ctx, callReq)
				if err != nil {
					resultText = fmt.Sprintf("tool error: %v", err)
				} else {
					for _, c := range toolResult.Content {
						if tc2, ok := c.(mcp.TextContent); ok {
							resultText += tc2.Text
						}
					}
				}

				messages = append(messages, ollamaMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    resultText,
				})
			}
		}

		// Send final "done" chunk
		writeChunkBuffered(w, chunk{Type: "done"}, &buffer, isCloudfFront)

		if isCloudfFront {
			flushBuffer(w, buffer)
		}
	}
}

// ── Main ───────────────────────────────────────────────────────────────────

func main() {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "mistral"
	}
	mcpURL := os.Getenv("MCP_URL")
	if mcpURL == "" {
		mcpURL = "http://localhost:3333/mcp-http"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3334"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	http.HandleFunc("/safecast-square-ct.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(logoPNG)
	})
	http.HandleFunc("/chat", handleChat(mcpURL, ollamaURL, model))
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	log.Printf("Safecast web-chat on :%s  MCP→%s  ollama=%s  model=%s", port, mcpURL, ollamaURL, model)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
