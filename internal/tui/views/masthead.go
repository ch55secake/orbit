package views

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

const saturn = "                     _.oo.\n" +
	"                 _.u[[/;:,.         .odMMMMMM'\n" +
	"              .o888UU[[[/;:-.  .o@P^    MMM^\n" +
	"             oN88888UU[[[/;::-.        dP^\n" +
	"            dNMMNN888UU[[[/;:--.   .o@P^\n" +
	"           ,MMMMMMN888UU[[/;::-. o@^\n" +
	"           NNMMMNN888UU[[[/~.o@P^\n" +
	"           888888888UU[[[/o@^-..\n" +
	"          oI8888UU[[[/o@P^:--..\n" +
	"       .@^  YUU[[[/o@^;::---..\n" +
	"     oMP     ^/o@P^;:::---..\n" +
	"  .dMMM    .o@^ ^;::---...\n" +
	" dMMMMMMM@^`       `^^^^\n" +
	"YMMMUP^\n" +
	" ^^"

func expanded(data DashboardData) bool {
	return data.Width >= 60 && data.Height >= 32
}

func showSaturn(data DashboardData) bool {
	return data.Width >= 60 && data.Height >= 40
}

func FleetInMasthead(width, height int) bool {
	return width >= 110 && height >= 40
}

func FleetWidth(width, height int) int {
	if FleetInMasthead(width, height) {
		return width - 4 - lipgloss.Width(saturn) - 2
	}
	return TableWidth(width)
}

// data.Width is the available content width, excluding outer padding.
func renderMasthead(data DashboardData) string {
	if showSaturn(data) {
		left := lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render("ORBIT")+"  "+styles.Subtitle.Render("v"+data.Version),
			styles.Subtitle.Render("Servers"),
		)
		if FleetInMasthead(data.Width+4, data.Height) {
			left = lipgloss.JoinVertical(lipgloss.Left, left, fleetSummary(data), "", data.Table)
		}
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
