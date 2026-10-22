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
		for _, want := range []string{"0.5", "1.25", "250.5", "Cores", "0:"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q: %q", want, plain)
			}
		}
		if width >= 56 {
			for _, want := range []string{"Load/4", "1m", "5m", "15m", "250.5↑", "█", "░"} {
				if !strings.Contains(strings.Split(plain, "\n")[0], want) {
					t.Fatalf("load meter missing %q: %q", want, plain)
				}
			}
		} else if !strings.Contains(plain, "Load 1/5/15m") {
			t.Fatalf("compact view lost load labels: %q", plain)
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
		if width == 80 && lipgloss.Width(view) != width {
			t.Fatal("core grid does not fill its own detail card")
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

func TestCoreColorScale(t *testing.T) {
	for _, test := range []struct {
		usage float64
		color string
	}{
		{usage: 0, color: "#30363d"},
		{usage: 1, color: "#0e4429"},
		{usage: 25, color: "#0e4429"},
		{usage: 25.1, color: "#006d32"},
		{usage: 50, color: "#006d32"},
		{usage: 50.1, color: "#26a641"},
		{usage: 75, color: "#26a641"},
		{usage: 75.1, color: "#39d353"},
		{usage: 100, color: "#39d353"},
	} {
		if got := coreShade(test.usage); got != test.color {
			t.Errorf("coreShade(%g) = %q, want %q", test.usage, got, test.color)
		}
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

func TestCoreLegendAndGridKeepCardBackground(t *testing.T) {
	data := client.MetricsData{CPU: client.CPU{
		LogicalCores:        4,
		PerCoreUsagePercent: []*float64{cpuSample(0), cpuSample(20), cpuSample(40), cpuSample(60)},
		LoadAverage:         &client.LoadAverage{OneMinute: cpuSample(1), FiveMinutes: cpuSample(2), FifteenMinutes: cpuSample(3)},
	}}
	rows := strings.Split(CPUDetails(data, 80), "\n")
	if len(rows) != 3 {
		t.Fatalf("unexpected core detail rows: %q", rows)
	}
	for _, row := range rows {
		assertCardTextBackground(t, row)
	}
}

func TestLoadMetersHandleMissingCoresInvalidSamplesAndOverload(t *testing.T) {
	load := &client.LoadAverage{OneMinute: nil, FiveMinutes: cpuSample(0), FifteenMinutes: cpuSample(8)}
	for _, width := range []int{56, 80} {
		view := CPUDetails(client.MetricsData{CPU: client.CPU{LogicalCores: 4, LoadAverage: load}}, width)
		rows := strings.Split(ansi.Strip(view), "\n")
		if len(rows) != 1 || lipgloss.Width(view) != width {
			t.Fatalf("load meter wrapped at width %d: %q", width, rows)
		}
		for _, want := range []string{"1m —", "5m", "15m", "8↑"} {
			if !strings.Contains(rows[0], want) {
				t.Fatalf("load meter missing %q at width %d: %q", want, width, rows[0])
			}
		}
		if strings.Count(rows[0], "—") != 2 || strings.Contains(rows[0], "NaN") {
			t.Fatalf("missing load looked like an idle sample: %q", rows[0])
		}
	}
	unknown := ansi.Strip(CPUDetails(client.MetricsData{CPU: client.CPU{LoadAverage: load}}, 80))
	if !strings.Contains(unknown, "Load 1/5/15m: — / 0 / 8") || strings.Contains(unknown, "█") {
		t.Fatalf("unknown core capacity produced a normalized meter: %q", unknown)
	}
}
