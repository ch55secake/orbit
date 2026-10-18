package views

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderServiceLogsBoundsTerminalAndSanitizesUntrustedMessages(t *testing.T) {
	entries := []client.ServiceLogEntry{{
		Timestamp: time.Date(2026, 10, 18, 12, 30, 0, 123456000, time.UTC),
		Priority:  intPointer(6),
		Message:   "\x1b[31mstarted\x1b[0m\nworker café 👩\u200d💻\x00\u202e ready",
		Cursor:    "opaque",
	}}
	for _, width := range []int{1, 2, 8, 20, 40, 80, 120, 180} {
		for _, height := range []int{1, 3, 8, 24, 40} {
			view := RenderServiceLogs(DashboardData{Width: width, Height: height}, ServiceLogsData{
				Server: "prod", Unit: "worker.service", Entries: entries, HasOlder: true, Following: true,
			})
			if lipgloss.Width(view) > width || lipgloss.Height(view) > height {
				t.Fatalf("log view overflowed %dx%d (%dx%d):\n%s", width, height, lipgloss.Width(view), lipgloss.Height(view), ansi.Strip(view))
			}
			if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\x00") || strings.Contains(view, "\u202e") {
				t.Fatalf("untrusted control escaped sanitization: %q", view)
			}
		}
	}
	plain := ansi.Strip(RenderServiceLogs(DashboardData{Width: 100, Height: 24}, ServiceLogsData{Server: "prod", Unit: "worker.service", Entries: entries}))
	if !strings.Contains(plain, "started") || !strings.Contains(plain, "worker café 👩\u200d💻") || !strings.Contains(plain, "ready") {
		t.Fatalf("multiline Unicode log content was not preserved: %q", plain)
	}
}

func TestServiceLogRowsWrapContinuationAndTruncation(t *testing.T) {
	entry := client.ServiceLogEntry{
		Timestamp:        time.Date(2026, 10, 18, 12, 30, 0, 0, time.UTC),
		Message:          "a very long log message that wraps over several columns界\nsecond line",
		MessageTruncated: true,
		Cursor:           "cursor",
	}
	rows := serviceLogRows([]client.ServiceLogEntry{entry}, 56)
	if len(rows) < 4 || !strings.Contains(rows[0], "2026-10-18T12:30:00Z") || !strings.Contains(rows[len(rows)-1], "message truncated") {
		t.Fatalf("unexpected wrapped log rows: %#v", rows)
	}
	for _, row := range rows[1:] {
		if strings.Contains(row, "2026-10-18T12:30:00Z") {
			t.Fatalf("continuation repeated metadata: %#v", rows)
		}
	}
}

func TestNarrowServiceLogRowsPreferReadableMessage(t *testing.T) {
	entry := client.ServiceLogEntry{Timestamp: time.Now(), Message: "started successfully", Cursor: "cursor"}
	rows := serviceLogRows([]client.ServiceLogEntry{entry}, 8)
	if !strings.Contains(strings.Join(rows, ""), "started") {
		t.Fatalf("narrow terminal metadata obscured the message: %#v", rows)
	}
}

func intPointer(value int) *int { return &value }
