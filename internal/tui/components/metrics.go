package components

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

func Metric(label, value string) string {
	return styles.Label.Render(label+":") + " " + styles.Value.Render(value)
}

// Cards gives metrics room to breathe without nesting more bordered panels.
func Cards(data client.MetricsData, width int) string {
	return SizedCards(data, width, 3)
}

// SizedCards expands the same metrics vertically when the terminal has room.
func SizedCards(data client.MetricsData, width, height int) string {
	if width < 56 {
		return Summary(data)
	}
	left := (width - 2) / 2
	right := width - 2 - left
	cpuCaption := "Utilization"
	if data.CPU.LogicalCores > 0 {
		cpuCaption = fmt.Sprintf("%d logical cores", data.CPU.LogicalCores)
	}
	card := func(metric, label, value, caption string, usage float64, size int) string {
		background := styles.Card.GetBackground()
		line := func(text string, style lipgloss.Style) string {
			return style.Background(background).Padding(0, 1).Width(size).Render(text)
		}
		finish := func(lines ...string) string {
			return lipgloss.NewStyle().Background(background).Width(size).Height(max(height, 3)).
				AlignVertical(lipgloss.Center).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
		}
		if !MetricAvailable(data, metric) {
			if height >= 5 {
				return finish(line(label, styles.Label), line("— unavailable", styles.Warn),
					line("", styles.Value), line("", styles.Value), line("No valid sample", styles.Subtitle))
			}
			return finish(
				line(label, styles.Label),
				line("— unavailable", styles.Warn),
				line("No valid sample", styles.Subtitle),
			)
		}
		if metric == "uptime" {
			if height >= 5 {
				return finish(line(label, styles.Label), line(value, styles.Title),
					line("", styles.Value), line("", styles.Value), line(caption, styles.Subtitle))
			}
			return finish(
				line(label, styles.Label), line(value, styles.Title), line(caption, styles.Subtitle),
			)
		}
		if height >= 5 {
			return finish(
				line(label, styles.Label), line(value, styles.Title), line("", styles.Value),
				line(usageBar(usage, size-2, background), styles.Value),
				line(ansi.Truncate(caption, size-2, "…"), styles.Subtitle),
			)
		}
		gap := max(size-2-lipgloss.Width(label)-lipgloss.Width(value), 1)
		return finish(
			line(label+strings.Repeat(" ", gap)+value, styles.Title),
			line(usageBar(usage, size-2, background), styles.Value),
			line(ansi.Truncate(caption, size-2, "…"), styles.Subtitle),
		)
	}
	cards := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top,
			card("cpu", "CPU", Percent(data.CPU.UsagePercent), cpuCaption, data.CPU.UsagePercent, left), "  ",
			card("memory", "Memory", Percent(data.Memory.UsedPercent), client.FormatBytes(data.Memory.UsedBytes)+" / "+client.FormatBytes(data.Memory.TotalBytes), data.Memory.UsedPercent, right),
		),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			card("disk", "Disk", Percent(data.Disk.UsedPercent), client.FormatBytes(data.Disk.UsedBytes)+" / "+client.FormatBytes(data.Disk.TotalBytes), data.Disk.UsedPercent, left), "  ",
			card("uptime", "Uptime", client.FormatUptime(data.Uptime), "since boot", 0, right),
		),
	)
	if swap := SwapDetails(data, width); swap != "" {
		cards = lipgloss.JoinVertical(lipgloss.Left, cards, swap)
	}
	return cards
}

func Percent(value float64) string {
	if !validPercent(value) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", value)
}

func Summary(data client.MetricsData) string {
	lines := []string{
		Metric("CPU", Percent(data.CPU.UsagePercent)+"  "+UsageBar(data.CPU.UsagePercent)),
		Metric("Memory", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes), Percent(data.Memory.UsedPercent))),
		Metric("Disk", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes), Percent(data.Disk.UsedPercent))),
		Metric("Uptime", client.FormatUptime(data.Uptime)),
	}
	for i, metric := range []string{"cpu", "memory", "disk", "uptime"} {
		if !MetricAvailable(data, metric) {
			lines[i] = Metric([]string{"CPU", "Memory", "Disk", "Uptime"}[i], "— unavailable")
		}
	}
	if swap := SwapSummary(data); swap != "" {
		lines = slices.Insert(lines, 2, swap)
	}
	return strings.Join(lines, "\n")
}

// SwapSummary leaves legacy/absent data out of the layout entirely. A failed
// collector remains explicitly unavailable, even if it supplied no swap object.
func SwapSummary(data client.MetricsData) string {
	if data.Swap == nil && !metricFailed(data, "swap") {
		return ""
	}
	if !MetricAvailable(data, "swap") {
		return Metric("Swap", "— unavailable")
	}
	if data.Swap.TotalBytes == 0 {
		return Metric("Swap", "not configured")
	}
	return Metric("Swap", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Swap.UsedBytes), client.FormatBytes(data.Swap.TotalBytes), Percent(data.Swap.UsedPercent)))
}

// SwapDetails renders an optional full-width card alongside the other meters.
// Legacy agents remain absent rather than appearing as zero-capacity hosts.
func SwapDetails(data client.MetricsData, width int) string {
	if data.Swap == nil && !metricFailed(data, "swap") {
		return ""
	}
	if width < 56 {
		return ansi.Wrap(SwapSummary(data), max(width, 1), "")
	}
	background := styles.Card.GetBackground()
	label := styles.Label.Background(background).Render("Swap")
	if !MetricAvailable(data, "swap") {
		return detailCard(width, label+"  "+styles.Warn.Background(background).Render("— unavailable"))
	}
	if data.Swap.TotalBytes == 0 {
		return detailCard(width, label+"  "+styles.Subtitle.Background(background).Render("not configured"))
	}
	value := styles.Title.Background(background).Render(Percent(data.Swap.UsedPercent))
	heading := label + "  " + value
	// Remaining capacity is derived from the same used/total sample as the meter;
	// optional or inconsistent free-byte readings must not contradict the bar.
	capacity := client.FormatBytes(data.Swap.UsedBytes) + " / " + client.FormatBytes(data.Swap.TotalBytes) +
		" used · " + client.FormatBytes(data.Swap.TotalBytes-data.Swap.UsedBytes) + " remaining"
	return detailCard(width, heading,
		usageBar(data.Swap.UsedPercent, min(width-2, 32), background),
		styles.Subtitle.Background(background).Render(ansi.Truncate(capacity, width-2, "…")),
	)
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

// MetricAvailable prevents failed collectors and invalid samples from appearing
// as healthy zero usage. Zero CPU usage and zero uptime are valid samples.
func MetricAvailable(data client.MetricsData, metric string) bool {
	return client.MetricAvailable(data, metric)
}

func metricFailed(data client.MetricsData, metric string) bool {
	return client.MetricFailed(data, metric)
}

// UsageBar renders a ten-cell meter outside a card.
func UsageBar(value float64) string {
	if !validPercent(value) {
		return styles.Subtitle.Render("unavailable")
	}
	return usageBar(value, 10, nil)
}

// Fractional blocks preserve sub-cell precision without rounding a near-full
// sample up to 100%. Every rendered cell keeps the same background.
func usageBar(value float64, width int, background color.Color) string {
	if width <= 0 {
		return ""
	}
	if !validPercent(value) {
		return styles.Subtitle.Background(background).Width(width).Render("—")
	}
	units := int(math.Floor(value / 100 * float64(width*8)))
	filled, fraction := units/8, units%8
	bar := strings.Repeat("█", filled)
	if fraction > 0 {
		bar += string([]rune("▏▎▍▌▋▊▉")[fraction-1])
	}
	style := styles.Good
	if value >= 90 {
		style = styles.Bad
	} else if value >= 70 {
		style = styles.Warn
	}
	return style.Background(background).Render(bar) + styles.Subtitle.Background(background).Render(strings.Repeat("░", width-lipgloss.Width(bar)))
}
