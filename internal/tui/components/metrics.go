package components

import (
	"fmt"
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
		Metric("CPU", Percent(data.CPU.UsagePercent)),
		Metric("Memory", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes), Percent(data.Memory.UsedPercent))),
		Metric("Disk", fmt.Sprintf("%s / %s (%s)", client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes), Percent(data.Disk.UsedPercent))),
		Metric("Uptime", client.FormatUptime(data.Uptime)),
	}, "  ")
}
