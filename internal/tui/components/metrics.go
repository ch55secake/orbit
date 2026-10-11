package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
)

func Metric(label, value string) string {
	return styles.Label.Render(label+":") + " " + styles.Value.Render(value)
}

func Percent(value float64) string {
	return fmt.Sprintf("%.1f%%", value)
}

func Summary(data client.MetricsData) string {
	return strings.Join([]string{
		Metric("CPU", Percent(data.CPU.UsagePercent)+"  "+UsageBar(data.CPU.UsagePercent)),
		Metric("Memory", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes), Percent(data.Memory.UsedPercent))),
		Metric("Disk", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes), Percent(data.Disk.UsedPercent))),
		Metric("Uptime", client.FormatUptime(data.Uptime)),
	}, "\n")
}

// UsageBar is a compact visual aid; the numeric percentage remains authoritative.
func UsageBar(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return styles.Subtitle.Render("unavailable")
	}
	value = math.Max(0, math.Min(100, value))
	filled := int(math.Round(value / 10))
	style := styles.Good
	if value >= 90 {
		style = styles.Bad
	} else if value >= 70 {
		style = styles.Warn
	}
	return style.Render(strings.Repeat("━", filled)) + styles.Subtitle.Render(strings.Repeat("─", 10-filled))
}
