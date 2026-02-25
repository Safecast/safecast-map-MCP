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

const systemPrompt = `You are a helpful assistant for the Safecast radiation monitoring network.
You have access to real-time radiation data from sensors around Japan and worldwide.
Use the available tools to answer questions about radiation levels, sensor locations, and measurement history.
Be concise but informative. Always mention measurement units (CPM or μSv/h) when reporting measurements.
When you don't have enough location context, ask the user to clarify.`

// ── Anthropic API types ────────────────────────────────────────────────────

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// contentBlock covers all content block variants we care about.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ── SSE helpers ────────────────────────────────────────────────────────────

type ssePayload struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

func sendSSE(w http.ResponseWriter, p ssePayload) {
	data, _ := json.Marshal(p)
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// ── Anthropic call ─────────────────────────────────────────────────────────

func callAnthropic(ctx context.Context, apiKey, model string, messages []anthropicMessage, tools []anthropicTool) (*anthropicResponse, error) {
	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     tools,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("anthropic %s: %s", ar.Error.Type, ar.Error.Message)
	}
	return &ar, nil
}

// ── MCP tool conversion ────────────────────────────────────────────────────

func mcpToolsToAnthropic(tools []mcp.Tool) []anthropicTool {
	var out []anthropicTool
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema)
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: json.RawMessage(schema),
		})
	}
	return out
}

// ── Chat handler ───────────────────────────────────────────────────────────

func handleChat(mcpURL, apiKey, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS preflight
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// SSE headers — X-Accel-Buffering: no prevents nginx from buffering the stream
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()

		var chatReq struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil || chatReq.Message == "" {
			sendSSE(w, ssePayload{Type: "error", Error: "invalid request: message required"})
			return
		}

		// ── Connect to MCP server ──────────────────────────────────────────
		mc, err := mcpclient.NewStreamableHttpClient(mcpURL)
		if err != nil {
			sendSSE(w, ssePayload{Type: "error", Error: fmt.Sprintf("MCP connect: %v", err)})
			return
		}
		defer mc.Close()

		if _, err := mc.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo:      mcp.Implementation{Name: "safecast-web-chat", Version: "1.0.0"},
			},
		}); err != nil {
			sendSSE(w, ssePayload{Type: "error", Error: fmt.Sprintf("MCP init: %v", err)})
			return
		}

		toolsResult, err := mc.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			sendSSE(w, ssePayload{Type: "error", Error: fmt.Sprintf("list tools: %v", err)})
			return
		}
		tools := mcpToolsToAnthropic(toolsResult.Tools)

		// ── Agentic loop ───────────────────────────────────────────────────
		messages := []anthropicMessage{
			{Role: "user", Content: chatReq.Message},
		}

		for {
			resp, err := callAnthropic(ctx, apiKey, model, messages, tools)
			if err != nil {
				sendSSE(w, ssePayload{Type: "error", Error: err.Error()})
				return
			}

			// Append assistant turn
			messages = append(messages, anthropicMessage{
				Role:    "assistant",
				Content: resp.Content,
			})

			// Collect tool_use blocks; stream text blocks
			var toolUses []contentBlock
			for _, block := range resp.Content {
				switch block.Type {
				case "text":
					sendSSE(w, ssePayload{Type: "text", Text: block.Text})
				case "tool_use":
					toolUses = append(toolUses, block)
				}
			}

			// No tool calls or end_turn → done
			if resp.StopReason == "end_turn" || len(toolUses) == 0 {
				break
			}

			// ── Execute tool calls via MCP ─────────────────────────────────
			var toolResults []contentBlock
			for _, tu := range toolUses {
				var args map[string]any
				_ = json.Unmarshal(tu.Input, &args)

				callReq := mcp.CallToolRequest{}
				callReq.Params.Name = tu.Name
				callReq.Params.Arguments = args

				var resultText string
				toolResult, err := mc.CallTool(ctx, callReq)
				if err != nil {
					resultText = fmt.Sprintf("tool error: %v", err)
				} else {
					for _, c := range toolResult.Content {
						if tc, ok := c.(mcp.TextContent); ok {
							resultText += tc.Text
						}
					}
				}

				toolResults = append(toolResults, contentBlock{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					Content:   resultText,
				})
			}

			// Append tool results as user turn
			messages = append(messages, anthropicMessage{
				Role:    "user",
				Content: toolResults,
			})
		}

		sendSSE(w, ssePayload{Type: "done"})
	}
}

// ── Main ───────────────────────────────────────────────────────────────────

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-5"
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
	http.HandleFunc("/chat", handleChat(mcpURL, apiKey, model))
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	log.Printf("Safecast web-chat on :%s  MCP→%s  model=%s", port, mcpURL, model)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
