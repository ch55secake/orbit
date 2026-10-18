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

func TestFilesystemsFitDashboard(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 40}, {Width: 40, Height: 40}, {Width: 80, Height: 24}, {Width: 80, Height: 32}, {Width: 80, Height: 40}, {Width: 120, Height: 24}, {Width: 120, Height: 40}} {
		for _, state := range []string{"legacy", "two", "many", "partial", "failed"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.Width, size.Height, state), func(t *testing.T) {
				m, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
				metrics := testMetrics(42)
				if state != "legacy" && state != "failed" {
					metrics.Data.Filesystems = []client.Filesystem{
						{MountPoint: "/", TotalBytes: 1024, UsedBytes: 512, UsedPercent: 50},
						{MountPoint: "/data", TotalBytes: 1024, UsedBytes: 972, UsedPercent: 95},
					}
				}
				if state == "many" {
					metrics.Data.Swap = &client.Swap{TotalBytes: 2147483648, UsedBytes: 536870912, UsedPercent: 25}
					for index := range 100 {
						metrics.Data.Filesystems = append(metrics.Data.Filesystems, client.Filesystem{MountPoint: fmt.Sprintf("/mnt/volume-%d-", index) + strings.Repeat("x", 80), TotalBytes: 1024, UsedBytes: 256, UsedPercent: 25})
					}
				}
				if state == "partial" {
					metrics.Data.Filesystems[0].TotalBytes = 0
				}
				if state == "partial" || state == "failed" {
					metrics.Data.Errors = []client.MetricError{{Metric: "filesystems", Message: "mount unavailable"}}
				}
				m.statuses[0].Metrics = &metrics
				m.Update(size)
				view := m.View().Content
				plain := ansi.Strip(view)
				if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
					t.Fatalf("filesystems overflow %dx%d: %dx%d\n%s", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view), plain)
				}
				if state == "legacy" && strings.Contains(plain, "FILESYSTEMS") {
					t.Fatal("legacy response gained filesystem detail")
				}
				if state != "legacy" && !strings.Contains(plain, "FILESYSTEMS") {
					t.Fatal("filesystem detail missing")
				}
				if state != "legacy" && state != "failed" && !strings.Contains(plain, "95.0%") {
					t.Fatal("fullest mount hidden")
				}
				for _, label := range []string{"CPU", "Memory", "Disk", "Uptime"} {
					if !strings.Contains(plain, label) {
						t.Fatalf("mount list hid %s", label)
					}
				}
			})
		}
	}
}
