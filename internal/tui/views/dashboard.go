package views

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/styles"
)

type DashboardData struct {
	Version        string
	ConfigPath     string
	Table          string
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

	var detail string
	switch {
	case data.Loading:
		detail = styles.Warn.Render("Loading metrics…")
	case data.Empty:
		detail = lipgloss.JoinVertical(lipgloss.Left,
			styles.Subtitle.Render("No servers configured"),
			styles.Label.Render("Add a server with: orbit servers add"),
		)
	case data.Selected == nil:
		detail = styles.Subtitle.Render("Select a server to inspect its metrics")
	case data.Selected.Error != nil:
		lines := []string{styles.Title.Render(data.Selected.Server.Name)}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, styles.Bad.Render(data.Selected.Error.Error()))
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	case data.Selected.Metrics != nil:
		metrics := data.Selected.Metrics.Data
		lines := []string{styles.Title.Render(data.Selected.Server.Name)}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines,
			styles.Subtitle.Render(metrics.Hostname+" · "+metrics.OS.Name+" "+metrics.OS.PlatformVersion),
			components.Summary(metrics),
		)
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	if data.Message != "" {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, styles.Bad.Render(data.Message))
	}
	if data.PasswordPrompt {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail,
			styles.Panel.Render(lipgloss.JoinVertical(lipgloss.Left,
				styles.Title.Render("SSH password required: "+data.PasswordServer),
				"Password: "+data.PasswordInput,
			)),
		)
	}

	footer := renderMenu(data)
	return strings.TrimSpace(lipgloss.JoinVertical(lipgloss.Left,
		header,
		configLine,
		"",
		styles.Panel.Render(data.Table),
		"",
		styles.Panel.Render(detail),
		"",
		footer,
	))
}

func renderMenu(data DashboardData) string {
	if data.PasswordPrompt {
		hints := []string{
			menuHint("enter", "submit"),
			menuHint("esc", "cancel"),
			menuHint("ctrl+c", "quit"),
		}
		if data.Width > 0 && data.Width < 50 {
			return stackMenuHints(hints)
		}
		return joinMenuHints(hints)
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
	if data.Width > 0 && data.Width < 50 {
		return stackMenuHints(hints)
	}
	return joinMenuHints(hints)
}

func stackMenuHints(hints []string) string {
	lines := make([]string, 0, (len(hints)+1)/2)
	for i := 0; i < len(hints); i += 2 {
		if i+1 < len(hints) {
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center, hints[i], "   ", hints[i+1]))
		} else {
			lines = append(lines, hints[i])
		}
	}
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
