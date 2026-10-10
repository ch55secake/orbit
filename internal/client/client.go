package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const APIVersion = "v1"

type Client struct {
	httpClient *http.Client
}

type HealthResponse struct {
	APIVersion string    `json:"api_version"`
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
}

type MetricsResponse struct {
	APIVersion string      `json:"api_version"`
	Timestamp  time.Time   `json:"timestamp"`
	Data       MetricsData `json:"data"`
}

type MetricsData struct {
	Hostname string        `json:"hostname"`
	OS       OSInfo        `json:"os"`
	CPU      CPU           `json:"cpu"`
	Memory   Memory        `json:"memory"`
	Disk     Disk          `json:"disk"`
	Uptime   uint64        `json:"uptime_seconds"`
	Network  Network       `json:"network"`
	Errors   []MetricError `json:"errors,omitempty"`
}

type MetricError struct {
	Metric  string `json:"metric"`
	Message string `json:"message"`
}

type OSInfo struct {
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	KernelArch      string `json:"kernel_arch"`
}

type CPU struct {
	UsagePercent float64 `json:"usage_percent"`
	LogicalCores int     `json:"logical_cores"`
}

type Memory struct {
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type Disk struct {
	Path        string  `json:"path"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type Network struct {
	BytesReceived   uint64 `json:"bytes_received"`
	BytesSent       uint64 `json:"bytes_sent"`
	PacketsReceived uint64 `json:"packets_received"`
	PacketsSent     uint64 `json:"packets_sent"`
}

type ErrorResponse struct {
	APIVersion string `json:"api_version"`
	Error      struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

func (c *Client) Health(ctx context.Context, address string) (HealthResponse, error) {
	var response HealthResponse
	err := c.get(ctx, address, "/api/v1/health", &response)
	return response, err
}

func (c *Client) Metrics(ctx context.Context, address string) (MetricsResponse, error) {
	var response MetricsResponse
	err := c.get(ctx, address, "/api/v1/metrics", &response)
	return response, err
}

func (c *Client) get(ctx context.Context, address, endpoint string, target any) error {
	base := strings.TrimRight(address, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return fmt.Errorf("server address %q must include http:// or https://", address)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var errorResponse ErrorResponse
		if decodeErr := json.NewDecoder(response.Body).Decode(&errorResponse); decodeErr == nil && errorResponse.Error.Code != "" {
			return fmt.Errorf("stardust returned %s: %s", errorResponse.Error.Code, errorResponse.Error.Message)
		}
		return fmt.Errorf("stardust returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func FormatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := float64(unit), 0
	for n := value / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/div, "KMGT"[exp])
}

func FormatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, hours)
	}
	return fmt.Sprintf("%02dh %02dm", hours, minutes)
}
