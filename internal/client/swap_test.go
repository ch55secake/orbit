package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestClientDecodesOptionalSwap(t *testing.T) {
	for _, test := range []struct {
		fixture           string
		present           bool
		total, used, free uint64
		percent           float64
		failed            bool
	}{
		{fixture: "metrics-complete.json"},
		{fixture: "metrics-partial.json"},
		{fixture: "metrics-swap.json", present: true, total: 2147483648, used: 536870912, free: 1610612736, percent: 25},
		{fixture: "metrics-no-swap.json", present: true},
		{fixture: "metrics-swap-unavailable.json", failed: true},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			server := fixtureServer(t, "/api/v1/metrics", http.StatusOK, readFixture(t, test.fixture))
			defer server.Close()
			response, err := New(time.Second).Metrics(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			swap := response.Data.Swap
			if (swap != nil) != test.present {
				t.Fatalf("optional swap = %+v", swap)
			}
			if swap != nil && *swap != (Swap{TotalBytes: test.total, UsedBytes: test.used, FreeBytes: test.free, UsedPercent: test.percent}) {
				t.Fatalf("swap values = %+v", swap)
			}
			if response.Data.Memory.UsedPercent != 37.5 || response.Data.CPU.UsagePercent != 23.5 {
				t.Fatal("optional swap changed existing metrics")
			}
			if test.failed && (len(response.Data.Errors) != 1 || response.Data.Errors[0].Metric != "swap") {
				t.Fatal("collector error lost")
			}
		})
	}
}

func TestNullSwapRemainsAbsent(t *testing.T) {
	var response MetricsResponse
	if err := json.Unmarshal([]byte(`{"api_version":"v1","data":{"swap":null}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Swap != nil {
		t.Fatal("null swap became a zero-capacity sample")
	}
}
