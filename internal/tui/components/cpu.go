package components

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// CPUDetails is additive: it never substitutes load averages for CPU usage.
// Squares follow logical core index order, not time; missing samples never look
// like idle cores. The aggregate CPU meter remains in the dashboard cards.
func CPUDetails(data client.MetricsData, width int) string {
	cpu := data.CPU
	if len(cpu.PerCoreUsagePercent) == 0 && cpu.LoadAverage == nil {
		return ""
	}
	if metricFailed(data, "cpu") {
		return ansi.Wrap(Metric("CPU detail", "— unavailable"), max(width, 1), "")
	}
	var lines []string
	if load := cpu.LoadAverage; load != nil {
		lines = append(lines, Metric("Load 1/5/15m", fmt.Sprintf("%s / %s / %s", loadValue(load.OneMinute), loadValue(load.FiveMinutes), loadValue(load.FifteenMinutes))))
	}
	if cores := cpu.PerCoreUsagePercent; len(cores) > 0 {
		graph, shown := coreContributionGraph(cores, width)
		legend := fmt.Sprintf("Cores · %d/%d", shown, len(cores))
		if shown < len(cores) {
			legend += fmt.Sprintf(" (+%d)", len(cores)-shown)
		}
		scale := " · 0% " + coreCell(0) + " " + coreCell(35) + " " + coreCell(70) + " " + coreCell(100) + " 100%"
		if lipgloss.Width(legend+scale) <= width {
			legend += scale
		}
		lines = append(lines, legend)
		lines = append(lines, graph...)
	}
	return ansi.Wrap(strings.Join(lines, "\n"), max(width, 1), "")
}

// coreContributionGraph maps cores in logical index order to a compact,
// two-row heatmap. The row prefixes make each square addressable as a core ID.
func coreContributionGraph(cores []*float64, width int) ([]string, int) {
	width = max(width, 1)
	digitWidth := len(strconv.Itoa(max(len(cores)-1, 0)))
	rowPrefixWidth := digitWidth + 2 // index and colon-space
	columns := max((width-rowPrefixWidth+1)/2, 1)
	shown := min(len(cores), columns*2)
	rows := make([]string, 0, 2)
	for start := 0; start < shown; start += columns {
		end := min(start+columns, shown)
		var row strings.Builder
		fmt.Fprintf(&row, "%0*d: ", digitWidth, start)
		for index := start; index < end; index++ {
			if index > start {
				row.WriteByte(' ')
			}
			row.WriteString(coreCellValue(cores[index]))
		}
		rows = append(rows, row.String())
	}
	return rows, shown
}

func coreCellValue(value *float64) string {
	if !coreAvailable(value) {
		return styles.Subtitle.Render("?")
	}
	return coreCell(*value)
}

func coreCell(value float64) string {
	var shade string
	switch {
	case value == 0:
		shade = "#30363d"
	case value <= 25:
		shade = "#0e4429"
	case value <= 50:
		shade = "#006d32"
	case value <= 75:
		shade = "#26a641"
	default:
		shade = "#39d353"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(shade)).Render("■")
}

func coreAvailable(value *float64) bool { return value != nil && validPercent(*value) }

func loadValue(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return "—"
	}
	// Load is not a percentage and may exceed 100; significant digits also keep
	// very large valid samples from consuming hundreds of terminal columns.
	return strconv.FormatFloat(*value, 'g', 4, 64)
}
