package views

import (
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

type TerminalGeometry struct{ X, Y, Width, Height int }

// TerminalBounds uses the same header, fleet, and footer measurements as Render.
// The PTY is sized to the inner cells of the resource/detail pane's shell frame.
func TerminalBounds(data DashboardData) TerminalGeometry {
	width := data.Width
	data.Width = max(width-4, 1)
	data.ShellActive = true
	header := renderMasthead(data)
	if !FleetInMasthead(width, data.Height) {
		header = lipgloss.JoinVertical(lipgloss.Left, header, fleetSummary(data))
	}
	frameWidth := data.Width
	frameHeight := data.Height - 4 - lipgloss.Height(header) - lipgloss.Height(renderMenu(data))
	x, y := 4, lipgloss.Height(header)+4
	if width >= 110 {
		frameWidth -= TableWidth(width) + 3
		x += TableWidth(width) + 3
	} else {
		frameHeight -= lipgloss.Height(data.Table) + 1
		y += lipgloss.Height(data.Table) + 1
	}
	return TerminalGeometry{X: x, Y: y, Width: max(frameWidth-4, 1), Height: max(frameHeight-3, 1)}
}

func renderTerminal(data DashboardData, width int) string {
	title := "SSH · " + data.ShellServer
	if data.ShellStatus != "" {
		title += " · " + data.ShellStatus
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		styles.Title.Render(ansi.Truncate(title, width, "…")),
		styles.Panel.Width(width).Render(data.ShellContent),
	)
}
