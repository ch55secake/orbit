package components

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func cpuSample(value float64) *float64 { return &value }

func TestCPUDetailsRenderCoreContributionGraphAndLoadUnits(t *testing.T) {
	data := client.MetricsData{CPU: client.CPU{UsagePercent: 23.5, LogicalCores: 4, PerCoreUsagePercent: []*float64{cpuSample(0), cpuSample(42.5), cpuSample(99.9), cpuSample(12)}, LoadAverage: &client.LoadAverage{OneMinute: cpuSample(0.5), FiveMinutes: cpuSample(1.25), FifteenMinutes: cpuSample(250.5)}}}
	for _, width := range []int{24, 40, 80} {
		view := CPUDetails(data, width)
		plain := ansi.Strip(view)
		for _, want := range []string{"Load 1/5/15m", "0.5", "1.25", "250.5", "Cores", "0:"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q: %q", want, plain)
			}
		}
		if strings.Count(plain, "■") < 4 {
			t.Fatalf("core utilization graph missing: %q", plain)
		}
		if width >= 56 && (!strings.Contains(plain, "0%") || !strings.Contains(plain, "100%")) {
			t.Fatalf("wide core graph is missing its usage legend: %q", plain)
		}
		if lipgloss.Width(view) > width {
			t.Fatal("CPU detail exceeds width")
		}
	}
	if *data.CPU.PerCoreUsagePercent[0] != 0 {
		t.Fatal("rendering reordered source CPU samples")
	}
}

func TestCoreGridKeepsLogicalOrderAndUnknownDistinct(t *testing.T) {
	cores := []*float64{cpuSample(0), cpuSample(25), cpuSample(50), cpuSample(75), cpuSample(100), nil}
	rows, shown := coreContributionGraph(cores, 11)
	if shown != len(cores) || len(rows) != 2 {
		t.Fatalf("core grid is not a two-row index-ordered graph: %#v (%d shown)", rows, shown)
	}
	if got := ansi.Strip(strings.Join(rows, "\n")); !strings.Contains(got, "0: ■ ■ ■ ■") || !strings.Contains(got, "4: ■ ?") {
		t.Fatalf("core grid reordered or hid a missing sample: %q", got)
	}
	if coreCell(0) == coreCell(100) || coreCell(25) == coreCell(75) || coreCellValue(nil) == coreCell(0) {
		t.Fatal("idle, busy, and missing cores have indistinguishable tiles")
	}
}

func TestCPUDetailsHandleMissingAndInvalidValues(t *testing.T) {
	if CPUDetails(client.MetricsData{}, 80) != "" {
		t.Fatal("legacy CPU gained detail")
	}
	data := client.MetricsData{CPU: client.CPU{PerCoreUsagePercent: []*float64{nil, cpuSample(-1), cpuSample(101), cpuSample(math.NaN())}, LoadAverage: &client.LoadAverage{OneMinute: nil, FiveMinutes: cpuSample(0), FifteenMinutes: cpuSample(-1)}}}
	view := ansi.Strip(CPUDetails(data, 80))
	for _, want := range []string{"— / 0 / —", "0: ? ? ? ?"} {
		if !strings.Contains(view, want) {
			t.Fatalf("invalid sample looks idle: %q", view)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		if loadValue(cpuSample(value)) != "—" {
			t.Fatal("invalid load accepted")
		}
	}
	if loadValue(cpuSample(1000)) != "1000" {
		t.Fatal("load incorrectly clamped as percentage")
	}
	data.Errors = []client.MetricError{{Metric: "CPU"}}
	if view := CPUDetails(data, 24); !strings.Contains(view, "unavailable") || strings.Contains(view, "0:—") {
		t.Fatal("failed CPU collector displayed samples")
	}
}

func TestManyCPUCoreSamplesStayBounded(t *testing.T) {
	var data client.MetricsData
	for range 256 {
		data.CPU.PerCoreUsagePercent = append(data.CPU.PerCoreUsagePercent, cpuSample(50))
	}
	for _, width := range []int{24, 40, 80} {
		view := CPUDetails(data, width)
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 4 {
			t.Fatal("large core list unbounded")
		}
		if !strings.Contains(ansi.Strip(view), "(+") {
			t.Fatal("omitted core count missing")
		}
	}
	data.CPU.LoadAverage = &client.LoadAverage{OneMinute: cpuSample(math.MaxFloat64)}
	if lipgloss.Height(CPUDetails(data, 24)) > 5 {
		t.Fatal("large load sample unbounded")
	}
}
