package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

func main() {
	listenAddr := ":8080"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		listenAddr = v
	}

	// Verify claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		log.Fatal("claude CLI not found in PATH")
	}
	log.Println("claude CLI found")

	http.HandleFunc("/api/chat", handleChat)
	http.HandleFunc("/", handleIndex)

	log.Printf("Server starting on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func handleChat(w http.ResponseWriter, r *http.Request) {
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

	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	log.Printf("User query: %s", msg)

	// Call claude CLI with the user's prompt
	// -p sends a single prompt (non-interactive)
	// --allowedTools allows MCP tools without interactive permission prompts
	cmd := exec.CommandContext(r.Context(), "claude",
		"-p", msg,
		"--output-format", "text",
		"--allowedTools", "mcp__claude_ai_Safecast_MCP__*",
	)
	// Remove CLAUDECODE env var to allow nested invocation
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("claude error: %v\noutput: %s", err, string(output))
		http.Error(w, fmt.Sprintf("claude error: %v", err), http.StatusInternalServerError)
		return
	}

	answer := strings.TrimSpace(string(output))
	log.Printf("Claude response: %.200s...", answer)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": answer})
}

func filterEnv(env []string, exclude string) []string {
	var out []string
	prefix := exclude + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Safecast MCP Chat (Claude)</title>
  <style>
    body { font-family: sans-serif; max-width: 700px; margin: 40px auto; padding: 0 20px; }
    #chat { border: 1px solid #ccc; padding: 16px; min-height: 300px; margin-bottom: 12px;
            overflow-y: auto; max-height: 500px; }
    .msg { margin-bottom: 12px; white-space: pre-wrap; }
    #input { width: calc(100% - 80px); padding: 8px; }
    button { padding: 8px 16px; }
    .user { color: #0066cc; }
    .assistant { color: #333; }
    .loading { color: #999; font-style: italic; }
  </style>
</head>
<body>
  <h2>Safecast MCP Chat (Claude CLI)</h2>
  <p style="color:#666;font-size:0.9em;">Type a question about radiation data. Claude will use Safecast MCP tools to answer.</p>
  <div id="chat"></div>
  <input id="input" placeholder="e.g. What is the radiation level near Fukushima?" onkeydown="if(event.key==='Enter')send()">
  <button onclick="send()">Send</button>
  <script>
    const chat = document.getElementById('chat');
    const input = document.getElementById('input');
    function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
    async function send() {
      const msg = input.value.trim();
      if (!msg) return;
      input.value = '';
      input.disabled = true;
      chat.innerHTML += '<div class="msg user"><b>You:</b> ' + esc(msg) + '</div>';
      chat.innerHTML += '<div class="msg loading" id="loading">Thinking... (Claude is querying MCP tools)</div>';
      chat.scrollTop = chat.scrollHeight;
      try {
        const res = await fetch('/api/chat', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({message: msg})
        });
        document.getElementById('loading')?.remove();
        if (!res.ok) {
          const text = await res.text();
          chat.innerHTML += '<div class="msg" style="color:red">Error: ' + esc(text) + '</div>';
        } else {
          const data = await res.json();
          chat.innerHTML += '<div class="msg assistant"><b>Claude:</b> ' + esc(data.response) + '</div>';
        }
      } catch(e) {
        document.getElementById('loading')?.remove();
        chat.innerHTML += '<div class="msg" style="color:red">Error: ' + esc(e.message) + '</div>';
      }
      input.disabled = false;
      input.focus();
      chat.scrollTop = chat.scrollHeight;
    }
  </script>
</body>
</html>`
