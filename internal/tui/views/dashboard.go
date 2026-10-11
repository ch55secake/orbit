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
	PasswordPrompt bool
	PasswordServer string
	PasswordInput  string
}

func Render(data DashboardData) string {
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		styles.Brand.Render("ORBIT"),
		"   ",
		styles.Title.Render("SERVER DASHBOARD"),
		"   ",
		styles.Subtitle.Render("v"+data.Version),
	)
	configLine := lipgloss.JoinHorizontal(lipgloss.Left,
		styles.Label.Render("CONFIG"),
		"  ",
		styles.Subtitle.Render(data.ConfigPath),
	)
	if data.Width > 0 && lipgloss.Width(header) > data.Width {
		header = lipgloss.JoinVertical(lipgloss.Left,
			styles.Brand.Render("ORBIT")+"   "+styles.Subtitle.Render("v"+data.Version),
			styles.Title.Render("SERVER DASHBOARD"),
		)
	}
	if data.Width > 0 {
		configLine = ansi.Truncate(configLine, data.Width, "…")
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
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Bad.Render("OFFLINE")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, styles.Bad.Render(data.Selected.Error.Error()), styles.Label.Render("Press r to retry the connection"))
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	case data.Selected.Metrics != nil:
		metrics := data.Selected.Metrics.Data
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Good.Render("ONLINE")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines,
			styles.Subtitle.Render(metrics.Hostname+" · "+metrics.OS.Name+" "+metrics.OS.PlatformVersion),
			"",
			components.Summary(metrics),
		)
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	if data.Message != "" {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, "", styles.Warn.Render(data.Message))
	}
	if detail == "" {
		detail = styles.Subtitle.Render("Waiting for metrics…")
	}
	sections := []string{
		header,
		configLine,
		"",
		fleetSummary(data),
		renderPanel(data.Table, data.Width),
		"",
	}
	if data.PasswordPrompt {
		sections = append(sections, renderPanel(lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render("SSH password required: "+data.PasswordServer),
			styles.Subtitle.Render("Used for this connection only; never saved."),
			"",
			"Password: "+data.PasswordInput,
		), data.Width))
	} else {
		sections = append(sections, renderPanel(detail, data.Width))
	}
	sections = append(sections,
		"",
		renderMenu(data),
	)
	return strings.TrimSpace(lipgloss.JoinVertical(lipgloss.Left, sections...))
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
	parts := []string{styles.Label.Render(fmt.Sprintf("SERVERS  %d", len(data.Statuses)))}
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
		return menuHint("q", "quit")
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
	hints = append(hints, menuHint("q", "quit"))
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
