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
	PasswordPrompt bool
	PasswordServer string
	PasswordInput  string
}

func Render(data DashboardData) string {
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		styles.Title.Render("ORBIT // MISSION CONTROL"),
		"  ",
		styles.Subtitle.Render("v"+data.Version),
	)
	configLine := styles.Subtitle.Render(data.ConfigPath)

	var detail string
	switch {
	case data.Loading:
		detail = styles.Warn.Render("Loading metrics…")
	case data.Empty:
		detail = styles.Subtitle.Render("No servers configured")
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
				styles.Subtitle.Render("enter submit   esc cancel"),
			)),
		)
	}

	footerText := "↑/k ↓/j navigate   r refresh   q quit"
	if data.PasswordPrompt {
		footerText = "enter submit   esc cancel   ctrl+c quit"
	}
	footer := styles.Footer.Render(footerText)
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
