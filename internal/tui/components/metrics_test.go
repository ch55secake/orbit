package components

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
)

func TestUsageBarIsBounded(t *testing.T) {
	for _, value := range []float64{-20, 0, 42, 70, 90, 100, 120} {
		if width := lipgloss.Width(UsageBar(value)); width != 10 {
			t.Errorf("UsageBar(%v) width = %d, want 10", value, width)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1)} {
		if !strings.Contains(UsageBar(value), "unavailable") {
			t.Errorf("UsageBar(%v) should be unavailable", value)
		}
	}
}

func TestSummaryStacksMetrics(t *testing.T) {
	summary := Summary(client.MetricsData{CPU: client.CPU{UsagePercent: 42}})
	lines := strings.Split(summary, "\n")
	if len(lines) != 4 {
		t.Fatalf("summary has %d lines, want 4: %q", len(lines), summary)
	}
	for i, label := range []string{"CPU:", "Memory:", "Disk:", "Uptime:"} {
		if !strings.Contains(lines[i], label) {
			t.Errorf("line %d missing %q: %q", i, label, lines[i])
		}
	}
}
