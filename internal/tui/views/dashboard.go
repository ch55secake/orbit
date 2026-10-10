package views

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/styles"
)

type DashboardData struct {
	Version    string
	ConfigPath string
	Table      string
	Selected   *server.Status
	Loading    bool
	Message    string
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
	case data.Selected == nil:
		detail = styles.Subtitle.Render("Select a server to inspect its metrics")
	case data.Selected.Error != nil:
		detail = styles.Bad.Render(fmt.Sprintf("%s: %v", data.Selected.Server.Name, data.Selected.Error))
	case data.Selected.Metrics != nil:
		metrics := data.Selected.Metrics.Data
		detail = lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render(data.Selected.Server.Name),
			styles.Subtitle.Render(metrics.Hostname+" · "+metrics.OS.Name+" "+metrics.OS.PlatformVersion),
			components.Summary(metrics),
		)
	}
	if data.Message != "" {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, styles.Bad.Render(data.Message))
	}

	footer := styles.Footer.Render("↑/k ↓/j navigate   r refresh   q quit")
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
