package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// detailCard gives optional dashboard details the same background and inset as
// metric cards. Wrap content before adding the background so a full-width meter
// is never rewrapped into an extra terminal row.
func detailCard(width int, lines ...string) string {
	if width < 4 {
		return ansi.Wrap(strings.Join(lines, "\n"), max(width, 1), "")
	}
	background := styles.Card.GetBackground()
	contentWidth := width - 2
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, row := range strings.Split(ansi.Wrap(line, contentWidth, ""), "\n") {
			rows = append(rows, lipgloss.NewStyle().Background(background).Padding(0, 1).Width(width).Render(row))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
