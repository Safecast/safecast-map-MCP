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

**Tool Priority — ALWAYS follow this order:**
1. User gives a device ID → sensor_current with device_id directly
2. User asks for readings/levels/data in an area → sensor_current with geographic bounds (returns actual CPM/µSv/h readings); then filter results by device type if requested
3. sensor_current returns empty → try device_history with the same device_id
4. User only wants to list/discover devices (no readings needed) → list_sensors
5. Historical data only → query_radiation, search_area, list_tracks

**CRITICAL:**
- sensor_current returns ACTUAL radiation readings (CPM values) — use for ALL "what is the reading" questions
- list_sensors returns metadata only (no CPM values) — NEVER use when readings are requested
- sensor_current has NO type filter — call it with geographic bounds, then filter results by type yourself
- NEVER use query_radiation for current/live data

**Device type names (use exactly as shown when filtering):**
- bGeigieZen → type: "geigiecast-zen"
- bGeigie → type: "geigiecast"
- Pointcast → type: "pointcast"
- Solarcast → type: "solarcast"
- Notehub/Blues → type: "notehub"

**Data Types**
- Real-time fixed stations: geigiecast-zen, pointcast, solarcast, notehub → use sensor_current
- Historical mobile surveys: geigiecast, bGeigie → use query_radiation, list_tracks
- CPM → µSv/h: multiply by ~0.0069 (LND 7318)

**Geographic queries:**
- sensor_current and search_area use bounding box (min_lat/max_lat/min_lon/max_lon), NOT lat+radius
- query_radiation uses lat/lon + radius_m
- Use your knowledge to estimate coordinates for named places; for a village/town use ±0.2° bounding box, for a city use ±0.5°, for a region use ±1-2°
- Example: "Mitsue, Nara" → min_lat=34.3, max_lat=34.7, min_lon=135.9, max_lon=136.5

**Formatting**
- Hide "_ai_generated_note" field (internal use only)
- Sensor data: ALWAYS use a markdown table with these exact columns: Device ID | Type | Location | Reading | Timestamp
- Device ID cell: clickable link → [geigiecast-zen:65002](https://simplemap.safecast.org/?lat=34.4827&lon=136.1631&zoom=15)
- Location cell: plain text coordinates ONLY, NO link → "34.48°N, 136.16°E" (2 decimal places)
- Reading cell: show both CPM and µSv/h → "42 CPM (0.29 µSv/h)"
- Timestamp cell: ALWAYS display in UTC — convert from any timezone, format as "2026-03-03 22:14 UTC"
- Example row:
  | [geigiecast-zen:65002](https://simplemap.safecast.org/?lat=34.4827&lon=136.1631&zoom=15) | bGeigieZen | 34.48°N, 136.16°E | 42 CPM (0.29 µSv/h) | 2026-03-03 22:14 |

Be concise. Ask for clarification if location unclear.`

// ── Mistral / OpenAI-compatible API types ─────────────────────────────────

type llmTool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string (OpenAI/Mistral format)
}

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function toolCallFunction `json:"function"`
}

type llmMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type llmRequest struct {
	Model    string       `json:"model"`
	Messages []llmMessage `json:"messages"`
	Tools    []llmTool    `json:"tools,omitempty"`
	Stream   bool         `json:"stream"`
}

type llmResponse struct {
	Choices []struct {
		Message      llmMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
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

// writeChunkBuffered either buffers the chunk or writes immediately
func writeChunkBuffered(w http.ResponseWriter, c chunk, buffer *[]chunk, useBuffer bool) {
	if useBuffer {
		*buffer = append(*buffer, c)
	} else {
		writeChunk(w, c)
	}
}

// flushBuffer writes all buffered chunks at once
func flushBuffer(w http.ResponseWriter, buffer []chunk) {
	for _, c := range buffer {
		writeChunk(w, c)
	}
}

// ── Mistral API call ───────────────────────────────────────────────────────

func callMistral(ctx context.Context, baseURL, apiKey, model string, messages []llmMessage, tools []llmTool) (*llmMessage, error) {
	reqBody := llmRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mistral request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var lr llmResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %.200s)", err, raw)
	}
	if lr.Error != nil {
		return nil, fmt.Errorf("mistral error: %s", lr.Error.Message)
	}
	if len(lr.Choices) == 0 {
		return nil, fmt.Errorf("empty response from mistral (body: %.200s)", raw)
	}
	msg := lr.Choices[0].Message
	return &msg, nil
}

// ── MCP tool conversion ────────────────────────────────────────────────────

func mcpToolsToLLM(tools []mcp.Tool) []llmTool {
	var out []llmTool
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema)
		var lt llmTool
		lt.Type = "function"
		lt.Function.Name = t.Name
		lt.Function.Description = t.Description
		lt.Function.Parameters = json.RawMessage(schema)
		out = append(out, lt)
	}
	return out
}

// ── Chat handler ───────────────────────────────────────────────────────────

func handleChat(mcpURL, baseURL, apiKey, model string) http.HandlerFunc {
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
			Message string       `json:"message"`
			History []llmMessage `json:"history,omitempty"`
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
		tools := mcpToolsToLLM(toolsResult.Tools)

		// ── Build message list ─────────────────────────────────────────────
		// System message first, then history, then new user message.
		messages := []llmMessage{
			{Role: "system", Content: systemPrompt},
		}
		messages = append(messages, chatReq.History...)
		messages = append(messages, llmMessage{Role: "user", Content: chatReq.Message})

		// ── Agentic loop ───────────────────────────────────────────────────
		for {
			msg, err := callMistral(ctx, baseURL, apiKey, model, messages, tools)
			if err != nil {
				writeChunkBuffered(w, chunk{Type: "error", Error: err.Error()}, &buffer, isCloudfFront)
				if isCloudfFront {
					flushBuffer(w, buffer)
				}
				return
			}

			// Ensure tool call types are set (Mistral may omit this field in responses)
			for i := range msg.ToolCalls {
				if msg.ToolCalls[i].Type == "" {
					msg.ToolCalls[i].Type = "function"
				}
			}

			// Append assistant message to history
			messages = append(messages, *msg)

			// Stream any text content
			if msg.Content != "" {
				writeChunkBuffered(w, chunk{Type: "text", Text: msg.Content}, &buffer, isCloudfFront)
			}

			// Stop if no tool calls
			if len(msg.ToolCalls) == 0 {
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

				messages = append(messages, llmMessage{
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
	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		log.Fatal("MISTRAL_API_KEY is required")
	}
	baseURL := os.Getenv("MISTRAL_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.mistral.ai"
	}
	model := os.Getenv("MISTRAL_MODEL")
	if model == "" {
		model = "mistral-small-latest"
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
	http.HandleFunc("/chat", handleChat(mcpURL, baseURL, apiKey, model))
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	log.Printf("Safecast web-chat on :%s  MCP→%s  base=%s  model=%s", port, mcpURL, baseURL, model)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
