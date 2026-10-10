package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsClientDecodesStardustResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/metrics" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_version":"v1","timestamp":"2026-01-01T00:00:00Z","data":{"hostname":"test","cpu":{"usage_percent":12.5}}}`))
	}))
	defer server.Close()

	response, err := New(2*1e9).Metrics(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if response.Data.Hostname != "test" || response.Data.CPU.UsagePercent != 12.5 {
		t.Fatalf("unexpected response: %+v", response)
	}
}
