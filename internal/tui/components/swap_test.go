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
			if test.swap != nil && test.swap.TotalBytes == 0 && MetricAvailable(data, "swap") {
				if SwapDetails(data, 80) != "" {
					t.Fatal("unconfigured swap created an optional card")
				}
			} else if test.want != "" && (test.swap == nil || !MetricAvailable(data, "swap")) {
				plain := SwapDetails(data, 80)
				if strings.Contains(plain, "\x1b[48;") || lipgloss.Height(plain) != 1 || ansi.Strip(plain) != test.want {
					t.Fatalf("unconfigured/failed swap created a background card: %q", plain)
				}
				for _, row := range strings.Split(SizedCards(data, 80, 5), "\n") {
					if strings.Contains(ansi.Strip(row), test.want) && strings.Contains(row, "\x1b[48;") {
						t.Fatalf("unconfigured/failed swap text inherited the card background: %q", row)
					}
				}
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
			} else if test.want != "" && test.name != "no swap" && !strings.Contains(cards, strings.TrimPrefix(test.want, "Swap: ")) {
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

func TestSwapMeterDoesNotWrapOrClaimMissingFreeBytes(t *testing.T) {
	for _, test := range []struct {
		percent float64
		used    uint64
		filled  int
	}{
		{percent: 0, used: 0, filled: 0},
		{percent: 25, used: 512 << 20, filled: 8},
		{percent: 100, used: 2 << 30, filled: 32},
	} {
		for _, width := range []int{56, 57, 80, 120} {
			data := client.MetricsData{Swap: &client.Swap{TotalBytes: 2 << 30, UsedBytes: test.used, UsedPercent: test.percent}}
			card := SwapDetails(data, width)
			rows := strings.Split(ansi.Strip(card), "\n")
			if len(rows) != 3 || lipgloss.Width(card) != width {
				t.Fatalf("swap meter wrapped at %d columns: %dx%d %q", width, lipgloss.Width(card), len(rows), rows)
			}
			if got := strings.Count(rows[1], "█"); got != test.filled {
				t.Fatalf("%.0f%% swap at width %d rendered %d filled cells, want %d: %q", test.percent, width, got, test.filled, rows[1])
			}
			if strings.Contains(strings.Join(rows, "\n"), "0 B free") {
				t.Fatal("missing free-byte sample presented as zero free capacity")
			}
		}
	}
}

func TestSwapCardSpacingOnlyWhenMeterIsRendered(t *testing.T) {
	data := client.MetricsData{Swap: &client.Swap{TotalBytes: 1024, UsedBytes: 256, UsedPercent: 25}}
	for _, height := range []int{3, 5} {
		withSwap := strings.Split(ansi.Strip(SizedCards(data, 80, height)), "\n")
		baseHeight := height*2 + 1
		gap := 0
		if height >= 5 {
			gap = 1
			if strings.TrimSpace(withSwap[baseHeight]) != "" {
				t.Fatal("expanded swap card has no visual separation")
			}
		}
		if len(withSwap) != baseHeight+gap+3 {
			t.Fatalf("configured swap has unexpected card spacing: %d rows", len(withSwap))
		}
		data.Swap = &client.Swap{}
		if got := lipgloss.Height(SizedCards(data, 80, height)); got != baseHeight {
			t.Fatalf("unconfigured swap left a phantom row: %d instead of %d", got, baseHeight)
		}
		data.Swap = &client.Swap{TotalBytes: 1024, UsedBytes: 256, UsedPercent: 25}
	}
}
