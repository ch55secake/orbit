package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureDir = "testdata/stardust/v1"

func TestClientDecodesStardustV1Fixtures(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		fixture   string
		kind      string
		wantStamp string
	}{
		{name: "health", path: "/api/v1/health", fixture: "health.json", kind: "health", wantStamp: "2026-10-11T12:34:56Z"},
		{name: "complete metrics", path: "/api/v1/metrics", fixture: "metrics-complete.json", kind: "metrics", wantStamp: "2026-10-11T12:34:56Z"},
		{name: "partial metrics", path: "/api/v1/metrics", fixture: "metrics-partial.json", kind: "metrics", wantStamp: "2026-10-11T12:34:56Z"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := readFixture(t, test.fixture)
			server := fixtureServer(t, test.path, http.StatusOK, body)
			defer server.Close()

			apiClient := New(time.Second)
			wantTimestamp, err := time.Parse(time.RFC3339, test.wantStamp)
			if err != nil {
				t.Fatalf("parse expected timestamp: %v", err)
			}

			switch test.kind {
			case "health":
				response, err := apiClient.Health(context.Background(), server.URL)
				if err != nil {
					t.Fatalf("health: %v", err)
				}
				if response.APIVersion != APIVersion || response.Status != "ok" || !response.Timestamp.Equal(wantTimestamp) {
					t.Fatalf("health response = %+v", response)
				}
			case "metrics":
				response, err := apiClient.Metrics(context.Background(), server.URL)
				if err != nil {
					t.Fatalf("metrics: %v", err)
				}
				if response.APIVersion != APIVersion || !response.Timestamp.Equal(wantTimestamp) {
					t.Fatalf("metrics envelope = %+v", response)
				}
				if response.Data.Hostname != "orbit-fixture" || response.Data.CPU.UsagePercent != 23.5 {
					t.Fatalf("metrics identity/CPU = %+v", response.Data)
				}
				if test.fixture == "metrics-complete.json" {
					assertCompleteMetricsFixture(t, response.Data)
				} else {
					assertPartialMetricsFixture(t, response.Data)
				}
			default:
				t.Fatalf("unknown response kind %q", test.kind)
			}
		})
	}
}

func TestClientRejectsStardustErrorsAndIncompatibleVersions(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		fixture    string
		body       string
		statusCode int
		kind       string
		wantError  string
	}{
		{
			name: "structured not found",
			path: "/api/v1/health", fixture: "error-not-found.json", statusCode: http.StatusNotFound,
			kind: "health", wantError: "stardust returned not_found: endpoint not found",
		},
		{
			name: "metrics unavailable",
			path: "/api/v1/metrics", fixture: "error-timeout.json", statusCode: http.StatusServiceUnavailable,
			kind: "metrics", wantError: "stardust returned metrics_timeout: metrics collection exceeded the configured timeout",
		},
		{
			name: "generic non-2xx response",
			path: "/api/v1/metrics", body: "upstream unavailable", statusCode: http.StatusBadGateway,
			kind: "metrics", wantError: "stardust returned HTTP 502",
		},
		{
			name: "health API version mismatch",
			path: "/api/v1/health", fixture: "health-v2.json", statusCode: http.StatusOK,
			kind: "health", wantError: `incompatible Stardust API version "v2" (supported: "v1")`,
		},
		{
			name: "metrics API version mismatch",
			path: "/api/v1/metrics", fixture: "metrics-v2.json", statusCode: http.StatusOK,
			kind: "metrics", wantError: `incompatible Stardust API version "v2" (supported: "v1")`,
		},
		{
			name: "error API version mismatch",
			path: "/api/v1/metrics", fixture: "error-v2.json", statusCode: http.StatusBadRequest,
			kind: "metrics", wantError: `incompatible Stardust API version "v2" (supported: "v1")`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := test.body
			if test.fixture != "" {
				body = readFixture(t, test.fixture)
			}
			server := fixtureServer(t, test.path, test.statusCode, body)
			defer server.Close()

			apiClient := New(time.Second)
			var err error
			switch test.kind {
			case "health":
				_, err = apiClient.Health(context.Background(), server.URL)
			case "metrics":
				_, err = apiClient.Metrics(context.Background(), server.URL)
			default:
				t.Fatalf("unknown response kind %q", test.kind)
			}
			if err == nil || err.Error() != test.wantError {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func assertCompleteMetricsFixture(t *testing.T, data MetricsData) {
	t.Helper()
	if data.OS.Name != "Ubuntu" || data.OS.PlatformVersion != "24.04" || data.OS.KernelArch != "x86_64" {
		t.Fatalf("OS fields = %+v", data.OS)
	}
	if data.CPU.LogicalCores != 8 || data.Memory.UsedBytes != 3221225472 || data.Memory.TotalBytes != 8589934592 || data.Memory.UsedPercent != 37.5 {
		t.Fatalf("CPU/memory fields = %+v / %+v", data.CPU, data.Memory)
	}
	if data.Disk.Path != "/" || data.Disk.UsedBytes != 10737418240 || data.Disk.TotalBytes != 42949672960 || data.Disk.UsedPercent != 25 {
		t.Fatalf("disk fields = %+v", data.Disk)
	}
	if data.Uptime != 93784 || data.Network.BytesReceived != 1048576 || data.Network.BytesSent != 2097152 || data.Network.PacketsReceived != 1500 || data.Network.PacketsSent != 1200 {
		t.Fatalf("uptime/network fields = %d / %+v", data.Uptime, data.Network)
	}
	if len(data.Errors) != 0 {
		t.Fatalf("complete metrics unexpectedly contain errors: %+v", data.Errors)
	}

	if got := FormatBytes(data.Memory.UsedBytes); got != "3.0 GiB" {
		t.Errorf("displayed memory = %q, want 3.0 GiB", got)
	}
	if got := FormatBytes(data.Disk.TotalBytes); got != "40.0 GiB" {
		t.Errorf("displayed disk total = %q, want 40.0 GiB", got)
	}
	if got := FormatUptime(data.Uptime); got != "1d 02h" {
		t.Errorf("displayed uptime = %q, want 1d 02h", got)
	}
	if got := fmt.Sprintf("%.1f%%", data.CPU.UsagePercent); got != "23.5%" {
		t.Errorf("displayed CPU = %q, want 23.5%%", got)
	}
}

func assertPartialMetricsFixture(t *testing.T, data MetricsData) {
	t.Helper()
	if data.Disk.UsedBytes != 0 || data.Disk.TotalBytes != 0 {
		t.Fatalf("unavailable disk metrics = %+v, want zero values", data.Disk)
	}
	if len(data.Errors) != 1 || data.Errors[0].Metric != "disk" || !strings.Contains(data.Errors[0].Message, "permission denied") {
		t.Fatalf("partial metric errors = %+v", data.Errors)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read Stardust fixture %q: %v", name, err)
	}
	return string(data)
}

func fixtureServer(t *testing.T, path string, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		if r.URL.Path != path {
			t.Errorf("request path = %q, want %q", r.URL.Path, path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept header = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}
