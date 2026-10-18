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
	Hostname    string        `json:"hostname"`
	OS          OSInfo        `json:"os"`
	CPU         CPU           `json:"cpu"`
	Memory      Memory        `json:"memory"`
	Swap        *Swap         `json:"swap,omitempty"`
	Disk        Disk          `json:"disk"`
	Filesystems []Filesystem  `json:"filesystems,omitempty"`
	Uptime      uint64        `json:"uptime_seconds"`
	Network     Network       `json:"network"`
	Errors      []MetricError `json:"errors,omitempty"`
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
	UsagePercent        float64      `json:"usage_percent"`
	LogicalCores        int          `json:"logical_cores"`
	PerCoreUsagePercent []*float64   `json:"per_core_usage_percent,omitempty"`
	LoadAverage         *LoadAverage `json:"load_average,omitempty"`
}

// Pointer samples distinguish unsupported/null values from valid zero load.
type LoadAverage struct {
	OneMinute      *float64 `json:"one_minute"`
	FiveMinutes    *float64 `json:"five_minutes"`
	FifteenMinutes *float64 `json:"fifteen_minutes"`
}

type Memory struct {
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// Swap is optional so older agents and unsupported platforms do not look like
// hosts with a valid zero-capacity swap sample.
type Swap struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type Disk struct {
	Path        string  `json:"path"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// Filesystem is an optional per-mount sample; Disk remains the configured path.
// Available bytes may exclude reserved space, so used + available can be less
// than total.
type Filesystem struct {
	MountPoint     string  `json:"mount_point"`
	Filesystem     string  `json:"filesystem,omitempty"`
	Device         string  `json:"device,omitempty"`
	UsedBytes      uint64  `json:"used_bytes"`
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type Network struct {
	BytesReceived   uint64             `json:"bytes_received"`
	BytesSent       uint64             `json:"bytes_sent"`
	PacketsReceived uint64             `json:"packets_received"`
	PacketsSent     uint64             `json:"packets_sent"`
	Interfaces      []NetworkInterface `json:"interfaces,omitempty"`
}

// NetworkInterface counters are cumulative, like the aggregate Network values.
type NetworkInterface struct {
	Name            string `json:"name"`
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
	if err := c.get(ctx, address, "/api/"+APIVersion+"/health", &response); err != nil {
		return response, err
	}
	if err := validateAPIVersion(response.APIVersion); err != nil {
		return response, err
	}
	return response, nil
}

func (c *Client) Metrics(ctx context.Context, address string) (MetricsResponse, error) {
	var response MetricsResponse
	if err := c.get(ctx, address, "/api/"+APIVersion+"/metrics", &response); err != nil {
		return response, err
	}
	if err := validateAPIVersion(response.APIVersion); err != nil {
		return response, err
	}
	return response, nil
}

func validateAPIVersion(version string) error {
	if version != APIVersion {
		return fmt.Errorf("incompatible Stardust API version %q (supported: %q)", version, APIVersion)
	}
	return nil
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
			if errorResponse.APIVersion != "" {
				if versionErr := validateAPIVersion(errorResponse.APIVersion); versionErr != nil {
					return versionErr
				}
			}
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
	const prefixes = "KMGTPE"
	for n := value / unit; n >= unit && exp < len(prefixes)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/div, prefixes[exp])
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
