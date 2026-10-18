package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

type statusMetric struct{ key, label, value string }

func printMetrics(out io.Writer, data client.MetricsData) error {
	metrics := []statusMetric{
		{"cpu", "CPU", fmt.Sprintf("%.1f%% (%d cores)", data.CPU.UsagePercent, data.CPU.LogicalCores)},
		{"memory", "Memory", fmt.Sprintf("%.1f%% (%s / %s)", data.Memory.UsedPercent, client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes))},
		{"disk", "Disk", fmt.Sprintf("%.1f%% (%s / %s)", data.Disk.UsedPercent, client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes))},
		{"uptime", "Uptime", client.FormatUptime(data.Uptime)},
	}
	if data.Swap != nil || client.MetricFailed(data, "swap") {
		value := "not configured"
		if swap := data.Swap; swap != nil && swap.TotalBytes > 0 {
			value = fmt.Sprintf("%.1f%% (%s / %s)", swap.UsedPercent, client.FormatBytes(swap.UsedBytes), client.FormatBytes(swap.TotalBytes))
		}
		metrics = slices.Insert(metrics, 2, statusMetric{"swap", "Swap", value})
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
	if err := printNetworkStatus(out, data); err != nil {
		return err
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

func printNetworkStatus(out io.Writer, data client.MetricsData) error {
	if !client.MetricAvailable(data, "network") {
		_, err := fmt.Fprintln(out, "  Network: unavailable")
		return err
	}
	network := data.Network
	if _, err := fmt.Fprintf(out, "  Network RX: %s (%d packets)\n  Network TX: %s (%d packets)\n", client.FormatBytes(network.BytesReceived), network.PacketsReceived, client.FormatBytes(network.BytesSent), network.PacketsSent); err != nil {
		return err
	}
	for _, iface := range network.Interfaces {
		name := plainStatusText(iface.Name)
		if name == "" {
			name = "(unnamed)"
		}
		if _, err := fmt.Fprintf(out, "  Interface %s: RX %s (%d packets), TX %s (%d packets)\n", name, client.FormatBytes(iface.BytesReceived), iface.PacketsReceived, client.FormatBytes(iface.BytesSent), iface.PacketsSent); err != nil {
			return err
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
