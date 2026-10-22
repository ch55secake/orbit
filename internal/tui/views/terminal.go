package views

import (
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type TerminalGeometry struct{ X, Y, Width, Height int }

func (geometry TerminalGeometry) Fits() bool { return geometry.Width >= 10 && geometry.Height >= 2 }

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
	return TerminalGeometry{X: x, Y: y, Width: frameWidth - 4, Height: frameHeight - 3}
}

func renderTerminalResizeWarning(data DashboardData) string {
	if data.Width <= 0 || data.Height <= 0 {
		return ""
	}
	title := "SSH · " + data.ShellServer
	if data.ShellUpdate {
		title = "UPDATE · " + data.ShellServer
	}
	status := data.ShellStatus
	if status == "" {
		status = "session kept open"
	}
	activity := "Resize terminal to show shell."
	closeHint := "Ctrl+] close SSH"
	if data.ShellUpdate {
		activity = "Resize terminal to show update."
		closeHint = "Ctrl+] close update details"
		if data.UpdateRunning {
			closeHint = "Update is running; please wait"
		}
	}
	lines := []string{
		styles.Title.Render(title + " · " + status),
		styles.Warn.Render(activity),
		styles.Subtitle.Render(closeHint),
	}
	if data.Height < len(lines) {
		lines = lines[len(lines)-data.Height:]
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, data.Width, "")
	}
	return strings.Join(lines, "\n")
}

func renderTerminal(data DashboardData, width int) string {
	title := "SSH · " + data.ShellServer
	if data.ShellUpdate {
		title = "UPDATE · " + data.ShellServer
	}
	if data.ShellStatus != "" {
		title += " · " + data.ShellStatus
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		styles.Title.Render(ansi.Truncate(title, width, "…")),
		styles.Panel.Width(width).Render(data.ShellContent),
	)
}
