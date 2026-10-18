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

func TestInterfacesFitDashboard(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 40}, {Width: 40, Height: 40}, {Width: 80, Height: 24}, {Width: 80, Height: 32}, {Width: 80, Height: 40}, {Width: 120, Height: 24}, {Width: 120, Height: 40}} {
		for _, count := range []int{0, 2, 100} {
			t.Run(fmt.Sprintf("%dx%d/%d", size.Width, size.Height, count), func(t *testing.T) {
				m, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
				metrics := testMetrics(42)
				if count == 100 {
					metrics.Data.Swap = &client.Swap{TotalBytes: 2147483648, UsedBytes: 536870912, UsedPercent: 25}
				}
				for index := range count {
					metrics.Data.Network.Interfaces = append(metrics.Data.Network.Interfaces, client.NetworkInterface{Name: fmt.Sprintf("veth-%d-", index) + strings.Repeat("x", 40), BytesReceived: 1048576, BytesSent: 2097152})
				}
				m.statuses[0].Metrics = &metrics
				m.Update(size)
				view := m.View().Content
				plain := ansi.Strip(view)
				if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
					t.Fatalf("interfaces overflow %dx%d: %dx%d\n%s", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view), plain)
				}
				if count == 0 && strings.Contains(plain, "INTERFACES") {
					t.Fatal("legacy response gained an interface list")
				}
				if count > 0 && (!strings.Contains(plain, "INTERFACES") || !strings.Contains(plain, "veth-0-")) {
					t.Fatal("interface detail missing")
				}
				for _, label := range []string{"CPU", "Memory", "Disk", "Uptime"} {
					if !strings.Contains(plain, label) {
						t.Fatalf("interface list hid %s", label)
					}
				}
			})
		}
	}
}

func TestInterfaceErrorsDoNotHideOtherMetrics(t *testing.T) {
	m, _ := testApp([]config.Server{testServer("alpha")})
	metrics := testMetrics(42)
	metrics.Data.Network.Interfaces = []client.NetworkInterface{{Name: "eth0", BytesReceived: 1048576}}
	metrics.Data.Errors = []client.MetricError{{Metric: "network", Message: "collection failed"}}
	m.statuses[0].Metrics = &metrics
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "unavailable") || strings.Contains(plain, "eth0") || !strings.Contains(plain, "42.0%") {
		t.Fatalf("partial failure rendered incorrectly:\n%s", plain)
	}
}
