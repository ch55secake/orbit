package components

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// InterfaceSummary bounds optional interface detail independently of fleet size.
// Aggregate counters stay unchanged; interface totals must not be summed since
// a packet can cross physical, bridge, and virtual interfaces on the same host.
func InterfaceSummary(data client.MetricsData, width int) string {
	interfaces := data.Network.Interfaces
	if len(interfaces) == 0 {
		return ""
	}
	if metricFailed(data, "network") {
		return ansi.Wrap(Metric("Network interfaces", "— unavailable"), max(width, 1), "")
	}
	lines := []string{styles.Label.Render("NETWORK INTERFACES · cumulative")}
	const limit = 2
	for _, iface := range interfaces[:min(len(interfaces), limit)] {
		name := strings.TrimSpace(strings.Map(func(character rune) rune {
			if unicode.IsControl(character) {
				return ' '
			}
			return character
		}, ansi.Strip(iface.Name)))
		if name == "" {
			name = "unnamed"
		}
		name = ansi.Truncate(name, max(min(width, 24), 1), "…")
		lines = append(lines, styles.Value.Render(fmt.Sprintf("%s  RX %s · TX %s", name, client.FormatBytes(iface.BytesReceived), client.FormatBytes(iface.BytesSent))))
	}
	if len(interfaces) > limit {
		lines = append(lines, styles.Subtitle.Render(fmt.Sprintf("+%d more interfaces", len(interfaces)-limit)))
	}
	return ansi.Wrap(strings.Join(lines, "\n"), max(width, 1), "")
}
