package client

import (
	"math"
	"strings"
)

// MetricFailed reports the collector errors carried by a partial v1 snapshot.
func MetricFailed(data MetricsData, metric string) bool {
	for _, failure := range data.Errors {
		if strings.EqualFold(failure.Metric, metric) {
			return true
		}
	}
	return false
}

// MetricAvailable keeps CLI and TUI availability consistent. Stardust's host
// collector also supplies uptime, so a host failure invalidates both. Valid zero
// CPU usage, zero uptime, and zero network counters remain legitimate samples.
func MetricAvailable(data MetricsData, metric string) bool {
	metric = strings.ToLower(metric)
	if MetricFailed(data, metric) {
		return false
	}
	percent := func(value float64) bool {
		return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
	}
	switch metric {
	case "cpu":
		return percent(data.CPU.UsagePercent)
	case "memory":
		return percent(data.Memory.UsedPercent) && data.Memory.TotalBytes > 0 && data.Memory.UsedBytes <= data.Memory.TotalBytes
	case "swap":
		swap := data.Swap
		return swap != nil && percent(swap.UsedPercent) && swap.UsedBytes <= swap.TotalBytes && swap.FreeBytes <= swap.TotalBytes && (swap.TotalBytes > 0 || swap.UsedPercent == 0)
	case "disk":
		return percent(data.Disk.UsedPercent) && data.Disk.TotalBytes > 0 && data.Disk.UsedBytes <= data.Disk.TotalBytes
	case "uptime":
		return !MetricFailed(data, "host")
	case "host", "network":
		return true
	default:
		return false
	}
}
