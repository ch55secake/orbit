package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func printMetrics(out io.Writer, data client.MetricsData) error {
	metrics := []struct{ key, label, value string }{
		{"cpu", "CPU", fmt.Sprintf("%.1f%% (%d cores)", data.CPU.UsagePercent, data.CPU.LogicalCores)},
		{"memory", "Memory", fmt.Sprintf("%.1f%% (%s / %s)", data.Memory.UsedPercent, client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes))},
		{"disk", "Disk", fmt.Sprintf("%.1f%% (%s / %s)", data.Disk.UsedPercent, client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes))},
		{"uptime", "Uptime", client.FormatUptime(data.Uptime)},
	}
	for _, metric := range metrics {
		value := metric.value
		if !client.MetricAvailable(data, metric.key) {
			value = "unavailable"
		}
		if _, err := fmt.Fprintf(out, "  %s: %s\n", metric.label, value); err != nil {
			return err
		}
	}
	if len(data.Errors) > 0 {
		if _, err := fmt.Fprintf(out, "  unavailable: %d metric(s)\n", len(data.Errors)); err != nil {
			return err
		}
		for _, failure := range data.Errors {
			if _, err := fmt.Fprintf(out, "    %s: %s\n", plainStatusText(failure.Metric), plainStatusText(failure.Message)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Collector messages and interface names are remote text, not terminal commands
// or extra output records. Keep them plain and on a single line.
func plainStatusText(text string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, ansi.Strip(text)))
}
