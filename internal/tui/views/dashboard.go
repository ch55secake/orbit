package views

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

type DashboardData struct {
	Version        string
	ConfigPath     string
	Table          string
	Statuses       []server.Status
	Selected       *server.Status
	Empty          bool
	Loading        bool
	Message        string
	Width          int
	ShowHelp       bool
	PasswordPrompt bool
	PasswordServer string
	PasswordInput  string
}

func Render(data DashboardData) string {
	terminalWidth := data.Width
	if data.Width > 0 {
		data.Width = max(data.Width-4, 1)
	}
	header := styles.Brand.Render("ORBIT") + "  " + styles.Subtitle.Render("Servers")
	version := styles.Subtitle.Render("v" + data.Version)
	gap := 3
	if data.Width > 0 {
		gap = max(data.Width-lipgloss.Width(header)-lipgloss.Width(version), 1)
	}
	header += strings.Repeat(" ", gap) + version
	if data.Width > 0 {
		header = ansi.Truncate(header, data.Width, "…")
	}
	detailWidth := data.Width
	if terminalWidth >= 110 {
		detailWidth = data.Width - TableWidth(terminalWidth) - 3
	}

	var detail string
	switch {
	case data.Loading && (data.Selected == nil || data.Selected.Metrics == nil):
		detail = styles.Warn.Render("Loading metrics…")
	case data.Empty:
		detail = lipgloss.JoinVertical(lipgloss.Left,
			styles.Subtitle.Render("No servers configured"),
			styles.Label.Render("Add a server with: orbit servers add"),
		)
	case data.Selected == nil:
		detail = styles.Subtitle.Render("Select a server to inspect its metrics")
	case data.Selected.Error != nil:
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Bad.Render("● offline")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, styles.Bad.Render(data.Selected.Error.Error()), styles.Label.Render("Press r to retry the connection"))
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	case data.Selected.Metrics != nil:
		metrics := data.Selected.Metrics.Data
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Good.Render("● online")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines,
			styles.Subtitle.Render(metrics.Hostname+" · "+metrics.OS.Name+" "+metrics.OS.PlatformVersion),
			"",
			components.Cards(metrics, detailWidth),
		)
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	if data.Message != "" {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, "", styles.Warn.Render(data.Message))
	}
	if detail == "" {
		detail = styles.Subtitle.Render("Waiting for metrics…")
	}
	if data.PasswordPrompt {
		detail = renderPanel(lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render("SSH password required: "+data.PasswordServer),
			styles.Subtitle.Render("Used for this connection only; never saved."),
			"",
			"Password: "+data.PasswordInput,
		), detailWidth)
	} else if detailWidth > 0 {
		detail = ansi.Wrap(detail, detailWidth, "")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, data.Table, "", detail)
	if terminalWidth >= 110 {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(TableWidth(terminalWidth)).Render(data.Table),
			"   ",
			detail,
		)
	}
	sections := []string{header, fleetSummary(data), "", body, "", renderMenu(data)}
	if data.ShowHelp && !data.PasswordPrompt {
		help := styles.Subtitle.Render("CONFIG  " + data.ConfigPath + "\n↑/k ↓/j navigate · r refresh · q quit · ? close help")
		if data.Width > 0 {
			help = ansi.Wrap(help, data.Width, "")
		}
		sections = append(sections, "", help)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// TableWidth is shared with the model so layout and column sizing stay in sync.
func TableWidth(terminalWidth int) int {
	if terminalWidth >= 110 {
		return min(50, max(38, (terminalWidth-4)/3))
	}
	return max(terminalWidth-4, 1)
}

func renderPanel(content string, width int) string {
	if width <= 0 {
		return styles.Panel.Render(content)
	}
	inner := max(width-4, 1)
	return styles.Panel.Width(width).Render(ansi.Wrap(content, inner, ""))
}

func fleetSummary(data DashboardData) string {
	online, offline := 0, 0
	for _, status := range data.Statuses {
		if status.Error != nil {
			offline++
		} else if status.Metrics != nil {
			online++
		}
	}
	parts := []string{styles.Label.Render(fmt.Sprintf("%d servers", len(data.Statuses)))}
	if online > 0 {
		parts = append(parts, styles.Good.Render(fmt.Sprintf("%d online", online)))
	}
	if offline > 0 {
		parts = append(parts, styles.Bad.Render(fmt.Sprintf("%d offline", offline)))
	}
	if data.Loading {
		parts = append(parts, styles.Warn.Render("Loading metrics…"))
	} else if pending := len(data.Statuses) - online - offline; pending > 0 {
		parts = append(parts, styles.Subtitle.Render(fmt.Sprintf("%d pending", pending)))
	}
	summary := strings.Join(parts, "   ")
	if data.Width > 0 {
		return ansi.Wrap(summary, data.Width, "")
	}
	return summary
}

func renderMenu(data DashboardData) string {
	if data.PasswordPrompt {
		hints := []string{
			menuHint("enter", "submit"),
			menuHint("esc", "cancel"),
			menuHint("ctrl+c", "quit"),
		}
		return fitMenuHints(hints, data.Width)
	}
	if data.Empty {
		return fitMenuHints([]string{menuHint("?", "help"), menuHint("q", "quit")}, data.Width)
	}
	upLabel, downLabel := "previous", "next"
	if data.Width > 0 && data.Width < 60 {
		upLabel, downLabel = "up", "down"
	}
	hints := []string{
		menuHint("↑ k", upLabel),
		menuHint("↓ j", downLabel),
	}
	if !data.Loading {
		hints = append(hints, menuHint("r", "refresh"))
	}
	hints = append(hints, menuHint("?", "help"), menuHint("q", "quit"))
	return fitMenuHints(hints, data.Width)
}

func fitMenuHints(hints []string, width int) string {
	if width <= 0 {
		return joinMenuHints(hints)
	}
	var lines []string
	line := ""
	for _, hint := range hints {
		if line != "" && lipgloss.Width(line)+3+lipgloss.Width(hint) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += hint
	}
	lines = append(lines, line)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func joinMenuHints(hints []string) string {
	items := make([]string, 0, len(hints)*2-1)
	for i, hint := range hints {
		if i > 0 {
			items = append(items, "   ")
		}
		items = append(items, hint)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, items...)
}

func menuHint(key, label string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center,
		styles.Key.Render(key),
		" ",
		styles.Footer.Render(label),
	)
}

func tunnelStatus(status *server.Status) string {
	if status.Server.SSH == nil {
		return ""
	}
	state := status.TunnelState
	if state == "" {
		state = "unknown"
	}
	return styles.Subtitle.Render("SSH tunnel: " + state)
}
