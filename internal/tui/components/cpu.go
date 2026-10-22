package components

import (
	"fmt"
	"image/color"
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
	card := width >= 56
	contentWidth := width
	if card {
		contentWidth -= 2
	}
	var background color.Color
	if card {
		background = styles.Card.GetBackground()
	}
	if metricFailed(data, "cpu") {
		if card {
			return detailCard(width, styles.Label.Background(background).Render("CORES"), styles.Warn.Background(background).Render("— unavailable"))
		}
		return ansi.Wrap(Metric("CPU detail", "— unavailable"), max(width, 1), "")
	}
	var lines []string
	if load := cpu.LoadAverage; load != nil {
		lines = append(lines, styles.Label.Background(background).Render("Load 1/5/15m: ")+styles.Value.Background(background).Render(fmt.Sprintf("%s / %s / %s", loadValue(load.OneMinute), loadValue(load.FiveMinutes), loadValue(load.FifteenMinutes))))
	}
	if cores := cpu.PerCoreUsagePercent; len(cores) > 0 {
		graph, shown := coreContributionGraphWithBackground(cores, contentWidth, background)
		legend := styles.Label.Background(background).Render("Cores") + styles.Value.Background(background).Render(fmt.Sprintf(" · %d/%d", shown, len(cores)))
		if shown < len(cores) {
			legend += styles.Subtitle.Background(background).Render(fmt.Sprintf(" (+%d)", len(cores)-shown))
		}
		spacer := styles.Label.Background(background)
		scale := spacer.Render(" · 0% ") + coreCellBackground(0, background) + spacer.Render(" ") + coreCellBackground(35, background) + spacer.Render(" ") + coreCellBackground(70, background) + spacer.Render(" ") + coreCellBackground(100, background) + spacer.Render(" 100%")
		if lipgloss.Width(legend+scale) <= contentWidth {
			legend += scale
		}
		lines = append(lines, legend)
		lines = append(lines, graph...)
	}
	if card {
		return detailCard(width, lines...)
	}
	return ansi.Wrap(strings.Join(lines, "\n"), max(width, 1), "")
}

// coreContributionGraph maps cores in logical index order to a compact,
// two-row heatmap. The row prefixes make each square addressable as a core ID.
func coreContributionGraph(cores []*float64, width int) ([]string, int) {
	return coreContributionGraphWithBackground(cores, width, nil)
}

func coreContributionGraphWithBackground(cores []*float64, width int, background color.Color) ([]string, int) {
	width = max(width, 1)
	digitWidth := len(strconv.Itoa(max(len(cores)-1, 0)))
	rowPrefixWidth := digitWidth + 2 // index and colon-space
	columns := max((width-rowPrefixWidth+1)/2, 1)
	shown := min(len(cores), columns*2)
	rows := make([]string, 0, 2)
	for start := 0; start < shown; start += columns {
		end := min(start+columns, shown)
		var row strings.Builder
		row.WriteString(styles.Label.Background(background).Render(fmt.Sprintf("%0*d: ", digitWidth, start)))
		for index := start; index < end; index++ {
			if index > start {
				row.WriteString(styles.Label.Background(background).Render(" "))
			}
			row.WriteString(coreCellValueBackground(cores[index], background))
		}
		rows = append(rows, row.String())
	}
	return rows, shown
}

func coreCellValue(value *float64) string {
	return coreCellValueBackground(value, nil)
}

func coreCellValueBackground(value *float64, background color.Color) string {
	if !coreAvailable(value) {
		return styles.Subtitle.Background(background).Render("?")
	}
	return coreCellBackground(*value, background)
}

func coreCell(value float64) string {
	return coreCellBackground(value, nil)
}

func coreCellBackground(value float64, background color.Color) string {
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
	return lipgloss.NewStyle().Foreground(lipgloss.Color(shade)).Background(background).Render("■")
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
