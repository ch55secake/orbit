package components

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestUsageBarIsBounded(t *testing.T) {
	for _, value := range []float64{0, 4, 42, 70, 90, 95, 99.9, 100} {
		if width := lipgloss.Width(UsageBar(value)); width != 10 {
			t.Errorf("UsageBar(%v) width = %d, want 10", value, width)
		}
	}
	for _, value := range []float64{-20, 120, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if !strings.Contains(UsageBar(value), "unavailable") {
			t.Errorf("UsageBar(%v) should be unavailable", value)
		}
		if Percent(value) != "—" {
			t.Errorf("Percent(%v) should not show an invalid reading", value)
		}
	}
}

func TestUsageBarFractionalFill(t *testing.T) {
	for _, test := range []struct {
		value float64
		want  string
	}{
		{0, "░░░░░░░░░░"},
		{4, "▍░░░░░░░░░"},
		{42, "████▏░░░░░"},
		{95, "█████████▌"},
		{99.9, "█████████▉"},
		{100, "██████████"},
	} {
		if got := ansi.Strip(UsageBar(test.value)); got != test.want {
			t.Errorf("UsageBar(%v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestUsageBarScalesWithoutOverstatingUsage(t *testing.T) {
	weights := map[rune]int{'█': 8, '▏': 1, '▎': 2, '▍': 3, '▌': 4, '▋': 5, '▊': 6, '▉': 7, '░': 0}
	for _, width := range []int{1, 2, 10, 25, 40} {
		for sample := 0; sample <= 1000; sample++ {
			value := float64(sample) / 10
			bar := ansi.Strip(usageBar(value, width, nil))
			if lipgloss.Width(bar) != width {
				t.Fatalf("bar width for %v/%d = %d", value, width, lipgloss.Width(bar))
			}
			units := 0
			for _, cell := range bar {
				weight, ok := weights[cell]
				if !ok {
					t.Fatalf("unexpected meter glyph %q", cell)
				}
				units += weight
			}
			want := int(math.Floor(value / 100 * float64(width*8)))
			if units != want || (value < 100 && units == width*8) {
				t.Fatalf("fill for %v/%d = %d, want %d", value, width, units, want)
			}
		}
	}
	if usageBar(42, 0, nil) != "" || usageBar(42, -1, nil) != "" {
		t.Fatal("nonpositive width should not render a meter")
	}
}

func TestUnavailableMetricsDoNotRenderHealthyGraphs(t *testing.T) {
	for index, metric := range []string{"cpu", "memory", "disk", "uptime"} {
		data := client.MetricsData{
			CPU:    client.CPU{UsagePercent: 37},
			Memory: client.Memory{UsedBytes: 512, TotalBytes: 1024, UsedPercent: 50},
			Disk:   client.Disk{UsedBytes: 1024, TotalBytes: 2048, UsedPercent: 50},
			Uptime: 3600,
			Errors: []client.MetricError{{Metric: metric, Message: "collection failed"}},
		}
		if MetricAvailable(data, metric) {
			t.Errorf("failed %s collector reported available", metric)
		}
		for _, view := range []string{Cards(data, 76), Summary(data)} {
			if strings.Count(view, "unavailable") != 1 {
				t.Errorf("failed %s metric missing unavailable state: %q", metric, view)
			}
		}
		line := strings.Split(ansi.Strip(Summary(data)), "\n")[index]
		if !strings.HasSuffix(line, "— unavailable") {
			t.Errorf("failed %s shows a value/graph: %q", metric, line)
		}
	}
	if !MetricAvailable(client.MetricsData{}, "cpu") {
		t.Fatal("genuine zero CPU usage should be available")
	}
	if MetricAvailable(client.MetricsData{}, "memory") || MetricAvailable(client.MetricsData{}, "disk") {
		t.Fatal("missing capacities should not appear as 0% utilization")
	}
	if MetricAvailable(client.MetricsData{CPU: client.CPU{UsagePercent: 101}}, "cpu") {
		t.Fatal("out-of-range percentage should be unavailable")
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

func TestMetricCardsFitAndRetainCapacity(t *testing.T) {
	data := client.MetricsData{
		CPU:    client.CPU{UsagePercent: 42},
		Memory: client.Memory{UsedBytes: 512, TotalBytes: 1024, UsedPercent: 50},
	}
	for _, width := range []int{56, 63, 76, 120} {
		cards := Cards(data, width)
		if got := lipgloss.Width(cards); got != width {
			t.Errorf("cards width = %d, want %d", got, width)
		}
		if height := lipgloss.Height(cards); height != 7 {
			t.Errorf("cards height = %d, want 7", height)
		}
		for _, want := range []string{"CPU", "Memory", "Disk", "Uptime", "42.0%", "512 B / 1.0 KiB"} {
			if !strings.Contains(cards, want) {
				t.Errorf("cards missing %q: %q", want, cards)
			}
		}
	}
}

func TestCardMetersFillTheirColumnWidth(t *testing.T) {
	data := client.MetricsData{
		CPU:    client.CPU{UsagePercent: 95},
		Memory: client.Memory{TotalBytes: 1024, UsedPercent: 0},
		Disk:   client.Disk{UsedBytes: 2048, TotalBytes: 2048, UsedPercent: 100},
	}
	for _, width := range []int{56, 63, 76, 120} {
		lines := strings.Split(ansi.Strip(Cards(data, width)), "\n")
		// Each pair has two horizontal padding cells per card plus a two-cell gap.
		if len([]rune(strings.TrimSpace(lines[1]))) != width-2 {
			t.Errorf("meter row does not span its card columns: %q", lines[1])
		}
		if !strings.Contains(lines[1], "░") || !strings.Contains(lines[1], "█") {
			t.Errorf("zero and near-full usage should have distinct meters: %q", lines[1])
		}
		if !strings.Contains(lines[5], "█") {
			t.Errorf("full disk meter missing: %q", lines[5])
		}
	}
}

func TestExtremeCapacitiesDoNotOverflowCards(t *testing.T) {
	data := client.MetricsData{
		Memory: client.Memory{UsedBytes: ^uint64(0), TotalBytes: ^uint64(0), UsedPercent: 100},
		Disk:   client.Disk{UsedBytes: ^uint64(0), TotalBytes: ^uint64(0), UsedPercent: 100},
		Uptime: ^uint64(0),
	}
	view := Cards(data, 56)
	if lipgloss.Width(view) != 56 || lipgloss.Height(view) != 7 {
		t.Fatalf("extreme values broke card layout: %q", view)
	}
}

func TestSizedCardsExpandWithoutChangingValues(t *testing.T) {
	data := client.MetricsData{
		CPU:    client.CPU{UsagePercent: 42},
		Memory: client.Memory{UsedBytes: 512, TotalBytes: 1024, UsedPercent: 50},
	}
	for _, height := range []int{3, 5, 8, 12} {
		view := SizedCards(data, 76, height)
		if got := lipgloss.Height(view); got != height*2+1 {
			t.Errorf("grid height = %d, want %d", got, height*2+1)
		}
		if lipgloss.Width(view) != 76 || !strings.Contains(view, "42.0%") || !strings.Contains(view, "512 B / 1.0 KiB") {
			t.Errorf("expanded cards lost values or overflowed: %q", view)
		}
	}
}
