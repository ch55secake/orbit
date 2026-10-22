package components

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
			if test.want == "" && (strings.Contains(Summary(data), "Swap") || SwapDetails(data, 80) != "") {
				t.Fatal("legacy response displays swap")
			}
			if test.want != "" && !strings.Contains(ansi.Strip(Summary(data)), test.want) {
				t.Fatal("compact summary omitted optional swap")
			}
			cards := ansi.Strip(SizedCards(data, 80, 5))
			if test.swap != nil && test.swap.TotalBytes > 0 && MetricAvailable(data, "swap") {
				for _, want := range []string{"Swap", Percent(test.swap.UsedPercent), client.FormatBytes(test.swap.UsedBytes) + " / " + client.FormatBytes(test.swap.TotalBytes)} {
					if !strings.Contains(cards, want) {
						t.Errorf("detailed swap bar omitted %q: %q", want, cards)
					}
				}
				if test.swap.UsedPercent > 0 && !strings.Contains(cards, "█") {
					t.Errorf("nonzero swap use missing filled meter: %q", cards)
				}
				if test.swap.UsedPercent < 100 && !strings.Contains(cards, "░") {
					t.Errorf("available swap missing meter remainder: %q", cards)
				}
			} else if test.want != "" && !strings.Contains(cards, strings.TrimPrefix(test.want, "Swap: ")) {
				t.Errorf("cards omitted optional swap status %q: %q", test.want, cards)
			}
		})
	}
}

func TestSwapDetailsFitAndShowAFullWidthMeter(t *testing.T) {
	data := client.MetricsData{Swap: &client.Swap{TotalBytes: 2 << 30, UsedBytes: 1 << 30, FreeBytes: 1 << 30, UsedPercent: 50}}
	for _, width := range []int{56, 80} {
		view := SwapDetails(data, width)
		if lipgloss.Width(view) != width || lipgloss.Height(view) != 3 {
			t.Fatalf("swap card is %dx%d, want %dx3: %q", lipgloss.Width(view), lipgloss.Height(view), width, ansi.Strip(view))
		}
		plain := strings.Join(strings.Fields(ansi.Strip(view)), " ")
		for _, want := range []string{"Swap", "50.0%", "1.0 GiB / 2.0 GiB used", "1.0 GiB remaining", "█", "░"} {
			if !strings.Contains(plain, want) {
				t.Errorf("swap details missing %q at width %d: %q", want, width, plain)
			}
		}
	}
	if narrow := SwapDetails(data, 20); lipgloss.Width(narrow) > 20 || !strings.Contains(ansi.Strip(narrow), "Swap:") {
		t.Fatalf("narrow swap fallback did not fit: %q", narrow)
	}
}
