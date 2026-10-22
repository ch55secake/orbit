package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestSwapFitsDashboard(t *testing.T) {
	for _, state := range []string{"legacy", "used", "none", "failed"} {
		for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 40}, {Width: 40, Height: 40}, {Width: 60, Height: 24}, {Width: 80, Height: 24}, {Width: 80, Height: 32}, {Width: 80, Height: 40}, {Width: 120, Height: 24}, {Width: 120, Height: 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", state, size.Width, size.Height), func(t *testing.T) {
				m, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
				metrics := testMetrics(42)
				want := ""
				switch state {
				case "used":
					metrics.Data.Swap = &client.Swap{TotalBytes: 2147483648, UsedBytes: 536870912, FreeBytes: 1610612736, UsedPercent: 25}
					want = "25.0%"
				case "none":
					metrics.Data.Swap = &client.Swap{}
					want = "not configured"
				case "failed":
					metrics.Data.Errors = []client.MetricError{{Metric: "swap", Message: "unsupported"}}
					want = "unavailable"
				}
				m.statuses[0].Metrics = &metrics
				m.Update(size)
				view := m.View().Content
				if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
					t.Fatalf("swap view overflows %dx%d: %dx%d\n%s", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view), ansi.Strip(view))
				}
				plain := ansi.Strip(view)
				if state == "legacy" && strings.Contains(plain, "Swap") {
					t.Fatal("older agent gained a fake swap reading")
				}
				if state == "none" && size.Width >= 60 {
					if strings.Contains(plain, "not configured") || strings.Contains(plain, "Swap") {
						t.Fatal("unconfigured swap should not create a wide-screen status block")
					}
				} else if state != "legacy" && (!strings.Contains(plain, "Swap") || !strings.Contains(plain, want)) {
					t.Fatalf("missing swap %q:\n%s", want, plain)
				}
				for _, label := range []string{"CPU", "Memory", "Disk", "Uptime"} {
					if !strings.Contains(plain, label) {
						t.Fatalf("swap obscured %s", label)
					}
				}
			})
		}
	}
}
