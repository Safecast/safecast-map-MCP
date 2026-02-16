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

	// Construct an enhanced prompt with tool usage guidelines
	enhancedPrompt := fmt.Sprintf(`When answering this question, follow these Safecast tool selection guidelines:

REAL-TIME DATA: When asked about "real-time", "current", "latest", or "live" sensor data, or when querying fixed sensors (Pointcast, Solarcast, bGeigieZen), you MUST use:
- sensor_current (for latest readings)
- sensor_history (for time-series data)

HISTORICAL DATA: Only use device_history for mobile bGeigie survey devices or historical import data.

UNIT CONVERSION REQUIREMENT:
Always present radiation measurements in µSv/h (microsieverts per hour), not CPM (counts per minute).
If data is provided in CPM, convert it using these detector-specific conversion factors:

Common Geiger-Müller tube conversion factors (CPM to µSv/h):
- LND 7317 (Pancake tube): µSv/h = CPM / 334
- SBM-20 (Russian tube): µSv/h = CPM / 175.43
- SBM-19: µSv/h = CPM / 108.3
- J305 (bGeigie standard): µSv/h = CPM / 100
- LND 78017: µSv/h = CPM / 294
- SI-22G: µSv/h = CPM / 108
- SI-3BG: µSv/h = CPM / 631

If the detector type is known, use its specific conversion factor. If unknown, note that the value is in CPM and conversion requires knowing the detector model.

User question: %s`, msg)

	// Call claude CLI with the enhanced prompt
	// -p sends a single prompt (non-interactive)
	// --allowedTools allows MCP tools without interactive permission prompts
	cmd := exec.CommandContext(r.Context(), "claude",
		"-p", enhancedPrompt,
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
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
  <style>
    * { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
           max-width: 800px; margin: 0 auto; padding: 20px; background: #f5f5f5; color: #333; }
    h2 { margin: 0 0 4px; }
    .subtitle { color: #666; font-size: 0.9em; margin: 0 0 16px; }

    #chat { background: #fff; border: 1px solid #ddd; border-radius: 8px; padding: 16px;
            min-height: 400px; max-height: 70vh; overflow-y: auto; margin-bottom: 12px; }

    .msg { margin-bottom: 16px; padding: 10px 14px; border-radius: 8px; }
    .msg:last-child { margin-bottom: 0; }

    .user { background: #e8f0fe; border-left: 3px solid #1a73e8; }
    .user .label { color: #1a73e8; font-weight: 600; margin-bottom: 4px; font-size: 0.85em; }

    .assistant { background: #f8f9fa; border-left: 3px solid #34a853; }
    .assistant .label { color: #34a853; font-weight: 600; margin-bottom: 4px; font-size: 0.85em; }
    .assistant .md-content h1, .assistant .md-content h2, .assistant .md-content h3 {
      margin-top: 12px; margin-bottom: 6px; }
    .assistant .md-content h1 { font-size: 1.3em; }
    .assistant .md-content h2 { font-size: 1.15em; }
    .assistant .md-content h3 { font-size: 1.05em; }
    .assistant .md-content p { margin: 6px 0; line-height: 1.5; }
    .assistant .md-content table { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 0.9em; }
    .assistant .md-content th, .assistant .md-content td {
      border: 1px solid #ddd; padding: 6px 10px; text-align: left; }
    .assistant .md-content th { background: #f0f0f0; font-weight: 600; }
    .assistant .md-content code { background: #e8e8e8; padding: 1px 5px; border-radius: 3px; font-size: 0.9em; }
    .assistant .md-content pre { background: #282c34; color: #abb2bf; padding: 12px; border-radius: 6px;
      overflow-x: auto; }
    .assistant .md-content pre code { background: none; color: inherit; padding: 0; }
    .assistant .md-content ul, .assistant .md-content ol { margin: 6px 0; padding-left: 24px; }
    .assistant .md-content li { margin: 3px 0; line-height: 1.5; }
    .assistant .md-content hr { border: none; border-top: 1px solid #ddd; margin: 12px 0; }
    .assistant .md-content em { color: #555; }

    .loading { color: #999; font-style: italic; padding: 10px 14px;
               animation: pulse 1.5s ease-in-out infinite; }
    @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.5; } }

    .error { background: #fde8e8; border-left: 3px solid #d93025; color: #d93025; }

    .input-row { display: flex; gap: 8px; }
    #input { flex: 1; padding: 10px 14px; border: 1px solid #ddd; border-radius: 6px;
             font-size: 1em; outline: none; }
    #input:focus { border-color: #1a73e8; box-shadow: 0 0 0 2px rgba(26,115,232,0.2); }
    #input:disabled { background: #f5f5f5; }
    button { padding: 10px 20px; background: #1a73e8; color: #fff; border: none;
             border-radius: 6px; font-size: 1em; cursor: pointer; }
    button:hover { background: #1557b0; }
  </style>
</head>
<body>
  <h2>Safecast MCP Chat</h2>
  <p class="subtitle">Ask about radiation data. Claude will query Safecast sensors via MCP tools.</p>
  <div id="chat"></div>
  <div class="input-row">
    <input id="input" placeholder="e.g. What is the radiation level near Fukushima?" onkeydown="if(event.key==='Enter')send()">
    <button onclick="send()">Send</button>
  </div>
  <script>
    const chat = document.getElementById('chat');
    const input = document.getElementById('input');
    function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
    async function send() {
      const msg = input.value.trim();
      if (!msg) return;
      input.value = '';
      input.disabled = true;
      chat.innerHTML += '<div class="msg user"><div class="label">You</div>' + esc(msg) + '</div>';
      chat.innerHTML += '<div class="msg loading" id="loading">Thinking\u2026 Claude is querying MCP tools</div>';
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
          chat.innerHTML += '<div class="msg error">Error: ' + esc(text) + '</div>';
        } else {
          const data = await res.json();
          const html = marked.parse(data.response);
          chat.innerHTML += '<div class="msg assistant"><div class="label">Claude</div><div class="md-content">' + html + '</div></div>';
        }
      } catch(e) {
        document.getElementById('loading')?.remove();
        chat.innerHTML += '<div class="msg error">Error: ' + esc(e.message) + '</div>';
      }
      input.disabled = false;
      input.focus();
      chat.scrollTop = chat.scrollHeight;
    }
  </script>
</body>
</html>`
