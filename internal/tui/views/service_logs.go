package views

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

type ServiceLogsData struct {
	Server, Unit string
	Entries      []client.ServiceLogEntry
	Scroll       int
	Loading      bool
	Following    bool
	Paused       bool
	HasOlder     bool
	Trimmed      bool
	Error        string
}

func ServiceLogsContentWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		terminalWidth = 80
	}
	padding := min(2, max((terminalWidth-1)/2, 0))
	return max(terminalWidth-2*padding, 1)
}

func ServiceLogsViewport(height int) int {
	if height <= 0 {
		height = 24
	}
	return max(height-4, 1)
}

func ServiceLogLineCount(entries []client.ServiceLogEntry, width int) int {
	return len(serviceLogRows(entries, max(width, 1)))
}

func RenderServiceLogs(data DashboardData, logs ServiceLogsData) string {
	terminalWidth, height := data.Width, data.Height
	if terminalWidth <= 0 {
		terminalWidth = 80
	}
	if height <= 0 {
		height = 24
	}
	padding := min(2, max((terminalWidth-1)/2, 0))
	width := max(terminalWidth-2*padding, 1)
	serverName := client.ServiceText(logs.Server)
	unitName := client.ServiceText(logs.Unit)
	title := styles.Title.Render("SERVICE LOGS · " + serverName + " · " + unitName)
	state := "history"
	if logs.Following {
		state = "following"
		if logs.Paused {
			state = "follow paused"
		}
	}
	status := fmt.Sprintf("%d entries · %s", len(logs.Entries), state)
	if logs.Trimmed {
		status += " · entries omitted"
	}
	header := []string{ansi.Truncate(title, width, "…"), ansi.Truncate(status, width, "…")}
	rows := serviceLogRows(logs.Entries, width)
	visible := ServiceLogsViewport(height)
	maxTop := max(len(rows)-visible, 0)
	top := logs.Scroll
	if top < 0 {
		top = maxTop
	}
	top = min(max(top, 0), maxTop)
	content := append([]string(nil), rows[top:min(top+visible, len(rows))]...)
	if len(content) == 0 {
		if logs.Loading {
			content = append(content, "Loading service logs…")
		} else if logs.Error != "" {
			content = append(content, styles.Warn.Render("Request failed: "+client.ServiceText(logs.Error)))
		} else {
			content = append(content, "No log entries in this time range.")
		}
	}
	if logs.Error != "" && len(rows) > 0 && len(content) < visible {
		content = append(content, styles.Warn.Render("Request failed: "+client.ServiceText(logs.Error)))
	}
	if logs.Loading && len(content) < visible {
		content = append(content, styles.Subtitle.Render("Loading…"))
	}
	footer := serviceLogsFooter(width, logs.HasOlder)
	lines := append(header, "")
	lines = append(lines, content...)
	lines = append(lines, styles.Subtitle.Render(footer))
	return lipgloss.NewStyle().Padding(0, padding).Render(serviceFit(lines, width, height))
}

func serviceLogsFooter(width int, hasOlder bool) string {
	if width < 14 {
		return "Esc"
	}
	if width < 21 {
		return "↑↓ f p Esc"
	}
	if width < 35 {
		return "↑↓ · f · p · r · Esc"
	}
	if width < 52 {
		if hasOlder {
			return "PgUp older · f · p · Esc"
		}
		return "f follow · p pause · Esc"
	}
	if width < 75 {
		if hasOlder {
			return "PgUp older · f · p · r · Tab server · Esc"
		}
		return "f follow · p pause · r · Tab server · Esc"
	}
	if hasOlder {
		return "↑↓ scroll · PgUp older · f follow · p pause · r refresh · Tab server · Esc services"
	}
	return "↑↓ scroll · f follow · p pause · r refresh · Tab server · Esc services"
}

func serviceLogRows(entries []client.ServiceLogEntry, width int) []string {
	rows := make([]string, 0, len(entries))
	for _, entry := range entries {
		priority := "-"
		priorityNumber := "-"
		if entry.Priority != nil {
			priority = serviceLogPriority(*entry.Priority)
			priorityNumber = fmt.Sprint(*entry.Priority)
		}
		prefix := fmt.Sprintf("%s %-7s │ ", entry.Timestamp.UTC().Format(time.RFC3339Nano), priority)
		switch {
		case width < 12:
			prefix = ""
		case width < 22:
			prefix = priorityNumber + " │ "
		case width < 42:
			prefix = entry.Timestamp.UTC().Format("15:04:05") + " " + priorityNumber + " │ "
		case width < 56:
			prefix = entry.Timestamp.UTC().Format("15:04:05.000") + " " + priorityNumber + " │ "
		}
		message := client.ServiceLogText(entry.Message)
		continuation := strings.Repeat(" ", ansi.StringWidth(prefix)) + "│ "
		if prefix == "" {
			continuation = ""
		}
		for index, line := range strings.Split(message, "\n") {
			wrapped := strings.Split(ansi.Hardwrap(line, max(width-ansi.StringWidth(continuation), 1), false), "\n")
			for wrapIndex, part := range wrapped {
				rowPrefix := continuation
				if index == 0 && wrapIndex == 0 {
					rowPrefix = prefix
				}
				rows = append(rows, rowPrefix+part)
			}
		}
		if entry.MessageTruncated {
			rows = append(rows, continuation+"… message truncated by Stardust")
		}
	}
	return rows
}

func serviceLogPriority(priority int) string {
	labels := [...]string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}
	if priority < 0 || priority >= len(labels) {
		return "-"
	}
	return labels[priority]
}
