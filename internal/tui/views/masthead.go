package views

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

const saturn = `  .                  *
           .-"""-.
      _.--' /   \ '--._
    .'     |     |     '.
   (        \   /        )
    '._      '-'      _.'
       '--.._____..--'     .`

const wordmark = `  ___  ____  ____ ___ _____
 / _ \|  _ \| __ )_ _|_   _|
| (_) | |_) |  _ \| |  | |
 \___/|_| \_\|___/___| |_|`

func expanded(data DashboardData) bool {
	return data.Width >= 60 && data.Height >= 32
}

// data.Width is the available content width, excluding outer padding.
func renderMasthead(data DashboardData) string {
	if expanded(data) {
		left := lipgloss.JoinVertical(lipgloss.Left,
			styles.Brand.Render(wordmark),
			"",
			styles.Title.Render("ORBIT")+"  "+styles.Subtitle.Render("v"+data.Version),
			styles.Subtitle.Render("Servers · local mission control"),
		)
		art := styles.Brand.Render(saturn)
		return lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(data.Width-lipgloss.Width(art)-2).Render(left),
			"  ", art,
		)
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
	return header
}
