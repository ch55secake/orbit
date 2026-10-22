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

func cpuSample(value float64) *float64 { return &value }

func TestCPUDetailsFitDashboard(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 40}, {Width: 40, Height: 40}, {Width: 80, Height: 24}, {Width: 80, Height: 32}, {Width: 80, Height: 40}, {Width: 120, Height: 24}, {Width: 120, Height: 40}} {
		for _, state := range []string{"legacy", "load", "cores", "partial", "many", "combined", "failed"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.Width, size.Height, state), func(t *testing.T) {
				m, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
				metrics := testMetrics(42)
				if state != "legacy" && state != "cores" {
					metrics.Data.CPU.LoadAverage = &client.LoadAverage{OneMinute: cpuSample(0.5), FiveMinutes: cpuSample(1.25), FifteenMinutes: cpuSample(2)}
				}
				if state != "legacy" && state != "load" {
					metrics.Data.CPU.PerCoreUsagePercent = []*float64{cpuSample(25), cpuSample(95)}
				}
				if state == "partial" {
					metrics.Data.CPU.LoadAverage.OneMinute = nil
					metrics.Data.CPU.PerCoreUsagePercent[0] = nil
				}
				if state == "many" || state == "combined" {
					for range 256 {
						metrics.Data.CPU.PerCoreUsagePercent = append(metrics.Data.CPU.PerCoreUsagePercent, cpuSample(50))
					}
				}
				if state == "combined" {
					metrics.Data.Swap = &client.Swap{TotalBytes: 2147483648, UsedBytes: 536870912, UsedPercent: 25}
					for index := range 100 {
						metrics.Data.Network.Interfaces = append(metrics.Data.Network.Interfaces, client.NetworkInterface{Name: fmt.Sprintf("veth-%d-", index) + strings.Repeat("x", 40), BytesReceived: 1048576, BytesSent: 2097152})
					}
				}
				if state == "failed" {
					metrics.Data.Errors = []client.MetricError{{Metric: "cpu", Message: "sample unavailable"}}
				}
				m.statuses[0].Metrics = &metrics
				m.Update(size)
				view := m.View().Content
				plain := ansi.Strip(view)
				if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
					t.Fatalf("CPU detail overflows %dx%d: %dx%d\n%s", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view), plain)
				}
				if state == "legacy" && (strings.Contains(plain, "Cores:") || strings.Contains(plain, "Load 1/5/15m")) {
					t.Fatal("legacy response gained CPU detail")
				}
				if state != "legacy" && state != "cores" && state != "failed" && !strings.Contains(plain, "Load 1/5/15m") {
					t.Fatal("load averages missing")
				}
				if state != "legacy" && state != "load" && state != "failed" && (!strings.Contains(plain, "Cores") || !strings.Contains(plain, "0:") || !strings.Contains(plain, "■")) {
					t.Fatal("core contribution graph or indices missing")
				}
				if state != "failed" && !strings.Contains(plain, "42.0%") {
					t.Fatal("CPU detail obscured aggregate CPU")
				}
				for _, label := range []string{"CPU", "Memory", "Disk", "Uptime"} {
					if !strings.Contains(plain, label) {
						t.Fatalf("CPU detail hid %s", label)
					}
				}
			})
		}
	}
}
