package client

import (
	"math"
	"testing"
)

func TestMetricAvailabilityHonorsStardustCollectors(t *testing.T) {
	data := MetricsData{CPU: CPU{UsagePercent: 0}, Memory: Memory{UsedBytes: 25, TotalBytes: 100, UsedPercent: 25}, Disk: Disk{UsedBytes: 50, TotalBytes: 100, UsedPercent: 50}, Swap: &Swap{}}
	for _, metric := range []string{"cpu", "memory", "disk", "swap", "uptime", "host", "network"} {
		if !MetricAvailable(data, metric) {
			t.Fatalf("valid %s unavailable", metric)
		}
		failed := data
		failed.Errors = []MetricError{{Metric: metric}}
		if MetricAvailable(failed, metric) {
			t.Fatalf("failed %s available", metric)
		}
	}
	data.Errors = []MetricError{{Metric: "HOST"}}
	if MetricAvailable(data, "uptime") || MetricAvailable(data, "host") {
		t.Fatal("host error did not invalidate uptime/host")
	}
	if !MetricAvailable(data, "cpu") || !MetricAvailable(data, "disk") {
		t.Fatal("host failure invalidated unrelated metrics")
	}
}

func TestInvalidSamplesAreUnavailable(t *testing.T) {
	for _, value := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		data := MetricsData{CPU: CPU{UsagePercent: value}, Memory: Memory{TotalBytes: 100, UsedPercent: value}, Disk: Disk{TotalBytes: 100, UsedPercent: value}, Swap: &Swap{TotalBytes: 100, UsedPercent: value}}
		for _, metric := range []string{"cpu", "memory", "disk", "swap"} {
			if MetricAvailable(data, metric) {
				t.Fatalf("invalid %s accepted: %v", metric, value)
			}
		}
	}
	data := MetricsData{Memory: Memory{UsedBytes: 101, TotalBytes: 100}, Disk: Disk{UsedBytes: 101, TotalBytes: 100}}
	if MetricAvailable(data, "memory") || MetricAvailable(data, "disk") || MetricAvailable(data, "swap") || MetricAvailable(data, "unknown") {
		t.Fatal("missing or impossible sample accepted")
	}
}
