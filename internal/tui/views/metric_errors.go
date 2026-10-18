package views

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// metricErrorWarnings keeps partial collector failures useful without allowing
// repeated errors (notably one per unreadable mount) to take over the dashboard.
func metricErrorWarnings(failures []client.MetricError, width int) []string {
	type group struct {
		name     string
		messages []string
	}
	groups := make([]group, 0, len(failures))
	indices := make(map[string]int, len(failures))
	for _, failure := range failures {
		name := strings.TrimSpace(ansi.Strip(failure.Metric))
		if name == "" {
			name = "metric"
		}
		key := strings.ToLower(name)
		// The filesystem panel already marks partial data and includes the
		// unreadable-mount count. Per-mount paths belong in `orbit status`, not
		// in the live dashboard warning area.
		if key == "filesystems" {
			continue
		}
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{name: name})
		}
		message := safeMetricText(failure.Message)
		groups[index].messages = append(groups[index].messages, message)
	}

	const maxWarnings = 3
	visible := min(len(groups), maxWarnings)
	warnings := make([]string, 0, visible+1)
	for _, item := range groups[:visible] {
		label := strings.ToUpper(safeMetricText(item.name))
		var message string
		switch {
		case len(item.messages) == 1:
			message = item.messages[0]
		default:
			message = item.messages[0] + fmt.Sprintf(" (+%d more)", len(item.messages)-1)
		}
		line := label + ": " + message
		if width > 0 {
			line = ansi.Truncate(line, width, "…")
		}
		warnings = append(warnings, styles.Warn.Render(line))
	}
	if len(groups) > visible {
		line := fmt.Sprintf("+%d more metric warnings · run orbit status for details", len(groups)-visible)
		if width > 0 {
			line = ansi.Truncate(line, width, "…")
		}
		warnings = append(warnings, styles.Warn.Render(line))
	}
	return warnings
}

func safeMetricText(text string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return ' '
		}
		return character
	}, ansi.Strip(text)))
}
