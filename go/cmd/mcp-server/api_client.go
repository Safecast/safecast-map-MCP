package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const safecastBaseURL = "https://api.safecast.org"

var client = NewSafecastClient()

type SafecastClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewSafecastClient() *SafecastClient {
	return &SafecastClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    safecastBaseURL,
	}
}

type MeasurementParams struct {
	Latitude        *float64
	Longitude       *float64
	Distance        *float64
	Limit           *int
	CapturedAfter   string
	CapturedBefore  string
	BGeigieImportID *int
	DeviceID        *int
}

func (c *SafecastClient) GetMeasurements(ctx context.Context, params MeasurementParams) ([]map[string]any, error) {
	v := url.Values{}
	if params.Latitude != nil {
		v.Set("latitude", strconv.FormatFloat(*params.Latitude, 'f', -1, 64))
	}
	if params.Longitude != nil {
		v.Set("longitude", strconv.FormatFloat(*params.Longitude, 'f', -1, 64))
	}
	if params.Distance != nil {
		v.Set("distance", strconv.FormatFloat(*params.Distance, 'f', -1, 64))
	}
	if params.Limit != nil {
		v.Set("limit", strconv.Itoa(*params.Limit))
	}
	if params.CapturedAfter != "" {
		v.Set("captured_after", params.CapturedAfter)
	}
	if params.CapturedBefore != "" {
		v.Set("captured_before", params.CapturedBefore)
	}
	if params.BGeigieImportID != nil {
		v.Set("bgeigie_import_id", strconv.Itoa(*params.BGeigieImportID))
	}
	if params.DeviceID != nil {
		v.Set("device_id", strconv.Itoa(*params.DeviceID))
	}
	return c.getList(ctx, "/measurements.json", v)
}

func (c *SafecastClient) GetBGeigieImports(ctx context.Context, limit int) ([]map[string]any, error) {
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	return c.getList(ctx, "/bgeigie_imports.json", v)
}

func (c *SafecastClient) GetBGeigieImport(ctx context.Context, id int) (map[string]any, error) {
	path := fmt.Sprintf("/bgeigie_imports/%d.json", id)
	return c.getOne(ctx, path)
}

func (c *SafecastClient) GetDevices(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, "/devices.json", nil)
}

func (c *SafecastClient) getList(ctx context.Context, path string, params url.Values) ([]map[string]any, error) {
	body, err := c.doGet(ctx, path, params)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return result, nil
}

func (c *SafecastClient) getOne(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.doGet(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return result, nil
}

func (c *SafecastClient) doGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no response from Safecast API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Safecast API error (%d): %s", resp.StatusCode, resp.Status)
	}

	return body, nil
}

// normalizeMeasurement extracts standard fields from a raw measurement map.
func normalizeMeasurement(m map[string]any) map[string]any {
	return map[string]any{
		"id":    m["id"],
		"value": m["value"],
		"unit":  m["unit"],
		"captured_at": m["captured_at"],
		"location": map[string]any{
			"latitude":  m["latitude"],
			"longitude": m["longitude"],
		},
		"device_id":  m["device_id"],
		"user_id":    m["user_id"],
		"height":     m["height"],
		"sensor_id":  m["sensor_id"],
		"station_id": m["station_id"],
	}
}

// jsonResult serializes v to indented JSON and returns it as a tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("failed to serialize response"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// floatPtr returns a pointer to a float64.
func floatPtr(f float64) *float64 { return &f }

// intPtr returns a pointer to an int.
func intPtr(i int) *int { return &i }
