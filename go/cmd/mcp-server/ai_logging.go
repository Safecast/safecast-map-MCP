package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type aiLogEvent struct {
	SessionID      string `json:"session_id"`
	Timestamp      string `json:"timestamp"`
	ToolName       string `json:"tool_name"`
	GeneratedQuery string `json:"generated_query"`
	DurationMs     int64  `json:"duration_ms"`
	CommitHash     string `json:"commit_hash"`
	Error          string `json:"error"`
}

var (
	gitCommitOnce sync.Once
	gitCommitHash string

	entireOnce     sync.Once
	entireEndpoint string
	entireAPIKey   string
	entireClient   *http.Client
)

// logAISession records a structured log entry for an AI tool execution.
// It is intentionally asynchronous and must not block the main tool handler.
func logAISession(toolName string, query string, duration int64, err error) {
	go func() {
		event := aiLogEvent{
			SessionID:      newSessionID(),
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			ToolName:       toolName,
			GeneratedQuery: sanitizeQuery(query),
			DurationMs:     duration,
			CommitHash:     getGitCommit(),
			Error:          errString(err),
		}

		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			log.Printf("failed to marshal AI log event: %v", marshalErr)
			return
		}

		// Always log locally.
		log.Println(string(data))

		// Optionally export to Entire if configured.
		initEntire()
		if entireEndpoint == "" || entireClient == nil {
			return
		}

		req, reqErr := http.NewRequest("POST", entireEndpoint, bytes.NewReader(data))
		if reqErr != nil {
			log.Printf("failed to create Entire export request: %v", reqErr)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if entireAPIKey != "" {
			req.Header.Set("Authorization", "Bearer "+entireAPIKey)
		}

		resp, httpErr := entireClient.Do(req)
		if httpErr != nil {
			log.Printf("failed to send log to Entire: %v", httpErr)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			log.Printf("Entire export returned non-success status: %d", resp.StatusCode)
		}
	}()
}

// executeWithLogging wraps a DuckDB query execution with consistent timing and logging.
// It must never panic and must not block tool execution (logging is asynchronous).
func executeWithLogging(
	toolName string,
	query string,
	fn func() (*sql.Rows, error),
) (*sql.Rows, error) {
	start := time.Now()
	rows, err := fn()
	duration := time.Since(start).Milliseconds()

	logAISession(toolName, query, duration, err)
	return rows, err
}

// getGitCommit returns the current git HEAD commit hash.
// It is cached after the first successful lookup.
func getGitCommit() string {
	gitCommitOnce.Do(func() {
		out, err := exec.Command("git", "rev-parse", "HEAD").Output()
		if err != nil {
			// Do not fail tool execution if git is unavailable.
			log.Printf("failed to read git commit hash: %v", err)
			return
		}
		gitCommitHash = strings.TrimSpace(string(out))
	})
	return gitCommitHash
}

// initEntire lazily initializes Entire export configuration and HTTP client.
func initEntire() {
	entireOnce.Do(func() {
		entireEndpoint = strings.TrimSpace(os.Getenv("ENTIRE_ENDPOINT"))
		entireAPIKey = strings.TrimSpace(os.Getenv("ENTIRE_API_KEY"))
		entireClient = &http.Client{
			Timeout: 2 * time.Second,
		}
	})
}

var singleQuotedLiteral = regexp.MustCompile(`'[^']*'`)

// sanitizeQuery normalizes whitespace and scrubs obvious literal values.
// This helps avoid logging sensitive data while retaining query structure.
func sanitizeQuery(q string) string {
	if q == "" {
		return ""
	}

	// Collapse all whitespace (including newlines) to single spaces.
	q = strings.Join(strings.Fields(q), " ")

	// Replace string literals with a placeholder.
	q = singleQuotedLiteral.ReplaceAllString(q, "'?'")

	// Truncate overly long queries to avoid oversized log lines.
	const maxLen = 1000
	if len(q) > maxLen {
		q = q[:maxLen] + "..."
	}

	return q
}

// errString returns a safe string representation of an error.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// newSessionID generates a RFC4122-ish random UUID v4 string.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// In the unlikely event of failure, return empty string rather than blocking.
		return ""
	}

	// Set version (4) and variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return formatUUID(b)
}

func formatUUID(b [16]byte) string {
	return strings.ToLower(
		strings.Join([]string{
			sprintf8(b[0:4]),
			sprintf4(b[4:6]),
			sprintf4(b[6:8]),
			sprintf4(b[8:10]),
			sprintf8(b[10:14]) + sprintf2(b[14:16]),
		}, "-"),
	)
}

func sprintf2(b []byte) string {
	return sprintfN(b, 2)
}

func sprintf4(b []byte) string {
	return sprintfN(b, 4)
}

func sprintf8(b []byte) string {
	return sprintfN(b, 4) // 4 bytes → 8 hex chars
}

func sprintfN(b []byte, byteCount int) string {
	if len(b) < byteCount {
		byteCount = len(b)
	}
	val := uint64(0)
	for i := 0; i < byteCount; i++ {
		val = (val << 8) | uint64(b[i])
	}
	width := byteCount * 2
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = "0123456789abcdef"[val&0xf]
		val >>= 4
	}
	return string(buf)
}

