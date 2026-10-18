package views

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

type ServiceBrowserData struct {
	Page             client.ServicePage
	Cursor           int
	Filter, Input    string
	Editing, Loading bool
	Error            string
	Detail           *client.ServiceUnit
	PageNumber       int
}

// RenderServices bounds every row and column, even in undersized terminals.
// Fleet/server context stays visible; no metrics or SSH state is discarded.
func RenderServices(data DashboardData, browser ServiceBrowserData) string {
	width, height := data.Width, data.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	terminalWidth := width
	horizontalPadding := min(2, max((terminalWidth-1)/2, 0))
	width = max(terminalWidth-2*horizontalPadding, 1)
	name := "Select a server"
	if data.Selected != nil {
		name = data.Selected.Server.Name
	}
	title := styles.Title.Render("SERVICES · " + client.ServiceText(name) + " · read-only")
	header := title
	fleetInHeader := terminalWidth >= 64 && height >= 40 && FleetInMasthead(terminalWidth, height)
	if terminalWidth >= 64 && height >= 40 {
		mastheadData := data
		mastheadData.Width = width
		header = renderMasthead(mastheadData) + "\n" + title
	}
	footer := serviceFooter(width, browser.Editing)
	headerLines := strings.Split(header, "\n")
	headerLines = headerLines[:min(len(headerLines), max(height-4, 1))]
	available := max(height-len(headerLines)-2, 1)
	fleetWidth := min(34, width/3)
	contentWidth := width
	if terminalWidth >= 100 && !fleetInHeader {
		contentWidth = width - fleetWidth - 3
	}
	lines := serviceLines(browser, contentWidth, available)
	if terminalWidth >= 100 && !fleetInHeader {
		fleet := []string{styles.Label.Render("FLEET · Tab changes server")}
		for index, status := range data.Statuses {
			prefix := "  "
			if index == data.SelectedIndex {
				prefix = "› "
			}
			fleet = append(fleet, prefix+client.ServiceText(status.Server.Name))
		}
		// Keep the selected server in the visible fleet window for large fleets.
		if len(fleet) > available && data.SelectedIndex >= available-1 {
			start := max(data.SelectedIndex-available+2, 0)
			fleet = append(fleet[:1:1], fleet[1+start:]...)
		}
		left := strings.Split(serviceFit(fleet, fleetWidth, available), "\n")
		right := strings.Split(serviceFit(lines, contentWidth, available), "\n")
		lines = nil
		for index := 0; index < max(len(left), len(right)); index++ {
			l, r := "", ""
			if index < len(left) {
				l = left[index]
			}
			if index < len(right) {
				r = right[index]
			}
			lines = append(lines, l+strings.Repeat(" ", max(fleetWidth-ansi.StringWidth(l), 0))+" │ "+r)
		}
	} else if !fleetInHeader {
		fleet := fmt.Sprintf("Fleet %d/%d · Tab/Shift+Tab changes server", data.SelectedIndex+1, len(data.Statuses))
		lines = append([]string{fleet}, serviceLines(browser, contentWidth, max(available-1, 1))...)
	}
	sections := append(headerLines, "")
	sections = append(sections, strings.Split(serviceFit(lines, width, available), "\n")...)
	// Always retain navigation hints rather than overflowing the terminal.
	if len(sections) >= height {
		sections = sections[:max(height-1, 0)]
	}
	sections = append(sections, styles.Subtitle.Render(footer))
	return lipgloss.NewStyle().Padding(0, horizontalPadding).Render(serviceFit(sections, width, height))
}

func serviceLines(browser ServiceBrowserData, width, height int) []string {
	filter := browser.Filter
	if browser.Editing {
		filter = browser.Input + "_"
	}
	lines := []string{fmt.Sprintf("Page %d · Filter: %s", browser.PageNumber, client.ServiceText(filter))}
	if browser.Loading {
		return append(lines, "Loading services…")
	}
	if browser.Error != "" {
		return append(lines, strings.Split(ansi.Hardwrap(client.ServiceText(browser.Error), max(width, 1), false), "\n")...)
	}
	if len(browser.Page.Units) == 0 {
		return append(lines, "No authorized services match this filter.")
	}
	selected := min(max(browser.Cursor, 0), len(browser.Page.Units)-1)
	unit := browser.Page.Units[selected]
	if browser.Detail != nil {
		unit = *browser.Detail
	}
	details := []string{
		styles.Label.Render("SELECTED · Enter/r reads live details"),
		client.ServiceText(unit.ID) + " · " + client.ServiceText(unit.Description),
		fmt.Sprintf("State: %s/%s · Load: %s", client.ServiceText(unit.ActiveState), client.ServiceText(unit.SubState), client.ServiceText(unit.LoadState)),
		"Unit file: " + client.ServiceText(unit.UnitFileState),
	}
	if unit.MainPID == 0 {
		details = append(details, "Main process: none")
	} else {
		details = append(details, fmt.Sprintf("Main PID: %d (live, not a stable process handle)", unit.MainPID))
	}
	if process := unit.MainProcess; process != nil {
		details = append(details, fmt.Sprintf("Process: %s · effective UID %d", client.ServiceText(process.Name), process.EffectiveUID))
	}
	detailRows := min(len(details), max(height/2, 0))
	listRows := min(len(browser.Page.Units), max(height-detailRows-2, 1))
	start := max(min(selected-listRows+1, len(browser.Page.Units)-listRows), 0)
	for index := start; index < start+listRows; index++ {
		entry := browser.Page.Units[index]
		prefix := "  "
		if index == selected {
			prefix = "› "
		}
		row := fmt.Sprintf("%s%s · %s/%s · PID %d", prefix, client.ServiceText(entry.Name), client.ServiceText(entry.ActiveState), client.ServiceText(entry.SubState), entry.MainPID)
		if index == selected {
			row = styles.Value.Render(row)
		}
		lines = append(lines, row)
	}
	if detailRows > 0 {
		lines = append(lines, "")
		lines = append(lines, details[:detailRows]...)
	}
	return lines
}

func serviceFit(lines []string, width, height int) string {
	lines = append([]string(nil), lines[:min(len(lines), max(height, 0))]...)
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, max(width, 1), "")
	}
	return strings.Join(lines, "\n")
}

func serviceFooter(width int, editing bool) string {
	if editing {
		if width < 12 {
			return "Esc"
		}
		if width < 25 {
			return "Enter · Esc"
		}
		if width < 36 {
			return "Enter apply · Esc cancel"
		}
		if width < 50 {
			return "Filter · Enter apply · Esc cancel"
		}
		return "Type a unit-name filter · Enter apply · Esc cancel"
	}
	switch {
	case width < 12:
		return "Esc back"
	case width < 22:
		return "r · Esc back"
	case width < 36:
		return "↑↓ · g logs · / · Esc back"
	case width < 56:
		return "↑↓ · g logs · / filter · Tab · Esc back"
	case width < 70:
		return "↑↓ · Enter · g logs · ←→ · / filter · Tab server · Esc back"
	case width < 90:
		return "↑↓ · Enter inspect · g logs · ←→ page · / filter · Tab server · Esc back"
	default:
		return "↑↓ select · Enter inspect · g logs · ←→ page · / filter · Tab server · Esc back"
	}
}
