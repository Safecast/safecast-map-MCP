// Package main provides the Safecast MCP server with an optional REST API layer.
//
// @title           Safecast API
// @version         1.0
// @description     REST access to the Safecast radiation monitoring dataset — 200M+ measurements from citizen scientists worldwide. All data is CC0-licensed and read-only. Powered by PostgreSQL+PostGIS with a Safecast REST API fallback.
// @contact.name    Safecast
// @contact.url     https://safecast.org
// @license.name    CC0 1.0 Universal
// @license.url     https://creativecommons.org/publicdomain/zero/1.0/
// @host            vps-01.safecast.jp
// @BasePath        /api
// @schemes         https http
//
// @tag.name        historical
// @tag.description Historical bGeigie mobile radiation measurements
// @tag.name        realtime
// @tag.description Real-time fixed sensor readings (Pointcast, Solarcast, bGeigieZen)
// @tag.name        spectroscopy
// @tag.description Gamma spectroscopy records
// @tag.name        reference
// @tag.description Aggregate statistics and reference information
package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	_ "github.com/your-org/safecast-mcp-server/cmd/mcp-server/docs"
	httpSwagger "github.com/swaggo/http-swagger"
)

//go:embed static/favicon.ico
var faviconICO []byte

//go:embed static/favicon-16x16.png
var favicon16 []byte

//go:embed static/favicon-32x32.png
var favicon32 []byte

// RESTHandler wires all REST API routes onto a mux.
type RESTHandler struct{}

// Register attaches all /api/* routes and the /docs/ Swagger UI to mux.
func (h *RESTHandler) Register(mux *http.ServeMux) {
	// Historical data
	mux.HandleFunc("/api/radiation", h.handleRadiation)
	mux.HandleFunc("/api/area", h.handleArea)
	mux.HandleFunc("/api/tracks", h.handleTracks)
	mux.HandleFunc("/api/track/", h.handleTrack) // /api/track/{id}
	mux.HandleFunc("/api/device/", h.handleDevice) // /api/device/{id}/history

	// Real-time sensors
	mux.HandleFunc("/api/sensors", h.handleSensors)
	mux.HandleFunc("/api/sensor/", h.handleSensor) // /api/sensor/{id}/current or /history

	// Spectroscopy
	mux.HandleFunc("/api/spectra", h.handleSpectra)
	mux.HandleFunc("/api/spectrum/", h.handleSpectrum) // /api/spectrum/{marker_id}

	// Reference / stats
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/info/", h.handleInfo) // /api/info/{topic}

	// Favicon endpoints
	mux.HandleFunc("/docs/favicon.ico", serveFavicon)
	mux.HandleFunc("/docs/favicon-16x16.png", serveFavicon16)
	mux.HandleFunc("/docs/favicon-32x32.png", serveFavicon32)

	// Swagger UI — themed to match simplemap admin pages
	mux.HandleFunc("/docs/swagger-theme.css", serveSwaggerTheme)
	mux.Handle("/docs/", httpSwagger.Handler(
		httpSwagger.URL("/docs/doc.json"),
		httpSwagger.UIConfig(map[string]string{
			"onComplete": `function() {
				// Change page title
				document.title = 'Safecast MCP Docs';

				// Inject Safecast favicon
				const link16 = document.createElement('link');
				link16.rel = 'icon';
				link16.type = 'image/png';
				link16.sizes = '16x16';
				link16.href = '/docs/favicon-16x16.png';
				document.head.appendChild(link16);

				const link32 = document.createElement('link');
				link32.rel = 'icon';
				link32.type = 'image/png';
				link32.sizes = '32x32';
				link32.href = '/docs/favicon-32x32.png';
				document.head.appendChild(link32);

				const linkICO = document.createElement('link');
				linkICO.rel = 'shortcut icon';
				linkICO.href = '/docs/favicon.ico';
				document.head.appendChild(linkICO);

				// Inject custom CSS
				const style = document.createElement('link');
				style.rel = 'stylesheet';
				style.href = '/docs/swagger-theme.css';
				document.head.appendChild(style);

				// Create dark mode toggle button
				const btn = document.createElement('button');
				btn.id = 'dark-mode-toggle';
				btn.textContent = '🌙 Dark Mode';

				// Check localStorage for saved preference
				const isDark = localStorage.getItem('darkMode') === 'true';
				if (isDark) {
					document.body.classList.add('dark-mode');
					btn.textContent = '☀️ Light Mode';
				}

				btn.onclick = function() {
					document.body.classList.toggle('dark-mode');
					const nowDark = document.body.classList.contains('dark-mode');
					btn.textContent = nowDark ? '☀️ Light Mode' : '🌙 Dark Mode';
					localStorage.setItem('darkMode', nowDark);
				};

				document.body.appendChild(btn);
			}`,
		}),
	))
}

// writeJSON writes v as a JSON response with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = jsonEncode(w, v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// jsonEncode writes v as JSON to w.
func jsonEncode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// serveMCPResult pipes an MCP tool result directly to an HTTP response.
// The tool functions already produce indented JSON, so we write the text content
// straight through. Tool errors become HTTP 400 responses.
func serveMCPResult(w http.ResponseWriter, result *mcp.CallToolResult, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil || len(result.Content) == 0 {
		writeError(w, http.StatusInternalServerError, "empty result")
		return
	}
	// Extract the text payload. Content is an interface; use AsTextContent to unwrap it.
	text := ""
	for _, c := range result.Content {
		if tc, ok := mcp.AsTextContent(c); ok && tc.Text != "" {
			text = tc.Text
			break
		}
	}
	if result.IsError {
		writeError(w, http.StatusBadRequest, text)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, text)
}

// serveFavicon serves the Safecast favicon.ico
func serveFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(faviconICO)
}

// serveFavicon16 serves the Safecast 16x16 favicon
func serveFavicon16(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(favicon16)
}

// serveFavicon32 serves the Safecast 32x32 favicon
func serveFavicon32(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(favicon32)
}

// serveSwaggerTheme serves the custom CSS that makes Swagger UI match simplemap's design.
func serveSwaggerTheme(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerThemeCSS))
}

// swaggerThemeCSS overrides Swagger UI defaults to match the simplemap admin theme.
// Colour values mirror those in admin-users.html and api-usage.html from safecast-new-map.
const swaggerThemeCSS = `
/* ── Safecast Swagger theme — matches simplemap admin pages ── */

/* Hide Swagger logo and collapse the space */
.swagger-ui .topbar-wrapper {
  padding-left: 20px !important;
}
.swagger-ui .topbar-wrapper img,
.swagger-ui .topbar-wrapper a {
  display: none !important;
}

/* Dark mode toggle button */
#dark-mode-toggle {
  position: fixed;
  top: 12px;
  right: 20px;
  z-index: 10000;
  padding: 8px 16px;
  background: #0066cc;
  color: white;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Arial, sans-serif;
  font-size: 14px;
  font-weight: 600;
  box-shadow: 0 2px 4px rgba(0,0,0,0.2);
  transition: background 0.2s;
}
#dark-mode-toggle:hover {
  background: #0055aa;
}
body.dark-mode #dark-mode-toggle {
  background: #1565c0;
}

/* Font stack and base background (light mode) */
body,
.swagger-ui,
.swagger-ui .wrapper {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Arial, sans-serif !important;
  background: #f5f5f5 !important;
  color: #333 !important;
  transition: background 0.3s, color 0.3s;
}

/* Top bar — matches --th-bg (#424242) from admin-users.html */
.swagger-ui .topbar {
  background: #424242 !important;
  padding: 8px 0 !important;
}
.swagger-ui .topbar .download-url-wrapper input[type=text] {
  border-radius: 8px !important;
}
.swagger-ui .topbar-wrapper a span {
  color: #fff !important;
  font-weight: 600 !important;
}

/* Info block */
.swagger-ui .info .title,
.swagger-ui .info h1,
.swagger-ui .info h2,
.swagger-ui .info h3 {
  color: #333 !important;
}

/* Operation blocks — card style matching --shadow and --btn-border-radius */
.swagger-ui .opblock {
  border-radius: 8px !important;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1) !important;
  margin-bottom: 8px !important;
  border: 1px solid #ddd !important;
  background: #fff !important;
}
.swagger-ui .opblock .opblock-summary {
  border-radius: 8px !important;
}
.swagger-ui .opblock.is-open {
  border-radius: 8px !important;
}

/* GET method colour — matches --link-color (#0066cc) */
.swagger-ui .opblock.opblock-get {
  border-color: #0066cc !important;
  background: #f0f6ff !important;
}
.swagger-ui .opblock.opblock-get .opblock-summary-method {
  background: #0066cc !important;
  border-radius: 4px !important;
}

/* Accent links */
.swagger-ui a,
.swagger-ui .opblock-summary-path,
.swagger-ui .info a {
  color: #0066cc !important;
}
.swagger-ui a:hover {
  text-decoration: underline !important;
}

/* Buttons */
.swagger-ui .btn.execute {
  background: #0066cc !important;
  border-color: #0066cc !important;
  border-radius: 8px !important;
  color: #fff !important;
}
.swagger-ui .btn.execute:hover {
  background: #0055aa !important;
}
.swagger-ui .btn.cancel {
  border-radius: 8px !important;
}
.swagger-ui .btn.authorize {
  border-radius: 8px !important;
  color: #0066cc !important;
  border-color: #0066cc !important;
}

/* Section headers */
.swagger-ui .opblock-tag {
  border-bottom: 1px solid #ddd !important;
  color: #333 !important;
  font-weight: 600 !important;
}

/* Parameter tables */
.swagger-ui table thead tr th {
  background: #424242 !important;
  color: #fff !important;
  font-weight: 600 !important;
}
.swagger-ui table tbody tr:hover {
  background: #f9f9f9 !important;
}

/* Response blocks */
.swagger-ui .responses-inner {
  background: #fff !important;
  border-radius: 8px !important;
}

/* Code blocks */
.swagger-ui .microlight,
.swagger-ui pre.microlight {
  background: #f6f8fa !important;
  border-radius: 8px !important;
  border: 1px solid #ddd !important;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace !important;
  font-size: 0.9rem !important;
}

/* Input fields */
.swagger-ui input[type=text],
.swagger-ui textarea,
.swagger-ui select {
  border-radius: 8px !important;
  border: 1px solid #ddd !important;
  background: #fff !important;
  color: #333 !important;
}

/* ── Dark mode — toggle via .dark-mode class ── */
body.dark-mode,
body.dark-mode .swagger-ui,
body.dark-mode .swagger-ui .wrapper {
  background: #1a1a1a !important;
  color: #eee !important;
}

body.dark-mode .swagger-ui .topbar {
  background: #616161 !important;
}

body.dark-mode .swagger-ui .info .title,
body.dark-mode .swagger-ui .info h1,
body.dark-mode .swagger-ui .info h2,
body.dark-mode .swagger-ui .info h3 {
  color: #eee !important;
}

body.dark-mode .swagger-ui .opblock {
  background: #2b2b2b !important;
  border-color: #444 !important;
  box-shadow: 0 1px 3px rgba(255, 255, 255, 0.07) !important;
}

body.dark-mode .swagger-ui .opblock.opblock-get {
  background: #1a2940 !important;
  border-color: #90caf9 !important;
}
body.dark-mode .swagger-ui .opblock.opblock-get .opblock-summary-method {
  background: #1565c0 !important;
}

body.dark-mode .swagger-ui a,
body.dark-mode .swagger-ui .opblock-summary-path,
body.dark-mode .swagger-ui .info a {
  color: #90caf9 !important;
}

body.dark-mode .swagger-ui .opblock-tag {
  border-bottom-color: #444 !important;
  color: #eee !important;
}

body.dark-mode .swagger-ui table thead tr th {
  background: #616161 !important;
}
body.dark-mode .swagger-ui table tbody tr:hover {
  background: #333 !important;
}
body.dark-mode .swagger-ui table tbody tr td {
  color: #eee !important;
  border-bottom-color: #444 !important;
}

body.dark-mode .swagger-ui .responses-inner {
  background: #2b2b2b !important;
}

body.dark-mode .swagger-ui .microlight,
body.dark-mode .swagger-ui pre.microlight {
  background: #161b22 !important;
  border-color: #444 !important;
  color: #e6edf3 !important;
}

body.dark-mode .swagger-ui input[type=text],
body.dark-mode .swagger-ui textarea,
body.dark-mode .swagger-ui select {
  background: #2b2b2b !important;
  border-color: #444 !important;
  color: #eee !important;
}

body.dark-mode .swagger-ui .btn.execute {
  background: #1565c0 !important;
  border-color: #1565c0 !important;
}
body.dark-mode .swagger-ui .btn.authorize {
  color: #90caf9 !important;
  border-color: #90caf9 !important;
}

body.dark-mode .swagger-ui .opblock-summary-description,
body.dark-mode .swagger-ui .parameter__name,
body.dark-mode .swagger-ui .parameter__type,
body.dark-mode .swagger-ui label {
  color: #eee !important;
}

body.dark-mode .swagger-ui section.models {
  background: #2b2b2b !important;
  border-color: #444 !important;
}

body.dark-mode .swagger-ui .scheme-container {
  background: #2b2b2b !important;
  border-color: #444 !important;
  box-shadow: none !important;
}

body.dark-mode .swagger-ui .schemes > label {
  color: #eee !important;
}

body.dark-mode .swagger-ui select {
  background: #2b2b2b !important;
  color: #eee !important;
  border-color: #444 !important;
}
`
