package components

import (
	"math"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestSwapSummary(t *testing.T) {
	for _, test := range []struct {
		name      string
		swap      *client.Swap
		errors    []client.MetricError
		want      string
		available bool
	}{
		{name: "legacy"},
		{name: "used", swap: &client.Swap{TotalBytes: 2147483648, UsedBytes: 536870912, FreeBytes: 1610612736, UsedPercent: 25}, want: "Swap: 512.0 MiB / 2.0 GiB (25.0%)", available: true},
		{name: "idle", swap: &client.Swap{TotalBytes: 1024, FreeBytes: 1024}, want: "Swap: 0 B / 1.0 KiB (0.0%)", available: true},
		{name: "no swap", swap: &client.Swap{}, want: "Swap: not configured", available: true},
		{name: "missing failed collector", errors: []client.MetricError{{Metric: "SWAP"}}, want: "Swap: — unavailable"},
		{name: "failed collector", swap: &client.Swap{}, errors: []client.MetricError{{Metric: "swap"}}, want: "Swap: — unavailable"},
		{name: "impossible used", swap: &client.Swap{TotalBytes: 100, UsedBytes: 101}, want: "Swap: — unavailable"},
		{name: "impossible free", swap: &client.Swap{TotalBytes: 100, FreeBytes: 101}, want: "Swap: — unavailable"},
		{name: "invalid percent", swap: &client.Swap{TotalBytes: 100, UsedPercent: 101}, want: "Swap: — unavailable"},
		{name: "nan percent", swap: &client.Swap{TotalBytes: 100, UsedPercent: math.NaN()}, want: "Swap: — unavailable"},
		{name: "infinite percent", swap: &client.Swap{TotalBytes: 100, UsedPercent: math.Inf(1)}, want: "Swap: — unavailable"},
		{name: "zero total with usage", swap: &client.Swap{UsedPercent: 1}, want: "Swap: — unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := client.MetricsData{Swap: test.swap, Errors: test.errors}
			if got := ansi.Strip(SwapSummary(data)); got != test.want {
				t.Fatalf("summary = %q, want %q", got, test.want)
			}
			if MetricAvailable(data, "swap") != test.available {
				t.Fatal("incorrect availability")
			}
			for _, view := range []string{Summary(data), SizedCards(data, 80, 5)} {
				if test.want == "" && strings.Contains(view, "Swap") {
					t.Fatal("legacy response displays swap")
				}
				if test.want != "" && !strings.Contains(ansi.Strip(view), test.want) {
					t.Fatal("summary/cards omitted optional swap")
				}
			}
		})
	}
}
