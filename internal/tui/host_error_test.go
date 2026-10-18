package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestHostCollectorFailureMarksUptimeUnavailable(t *testing.T) {
	m, _ := testApp([]config.Server{testServer("local")})
	response := testMetrics(42, client.MetricError{Metric: "host", Message: "host unavailable"})
	response.Data.Uptime = 0
	m.statuses[0].Metrics = &response
	m.table.SetRows(makeRows(m.cfg.Servers, m.statuses))
	if m.table.Rows()[0][5] != "—" {
		t.Fatal("host failure rendered healthy zero fleet uptime")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "Uptime") || !strings.Contains(plain, "unavailable") || strings.Contains(plain, "00h 00m") || !strings.Contains(plain, "42.0%") {
		t.Fatalf("host failure rendering:\n%s", plain)
	}
}
