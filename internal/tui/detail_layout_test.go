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

func TestCombinedOptionalCardsStayWithinDashboard(t *testing.T) {
	for _, swapConfigured := range []bool{false, true} {
		for _, width := range []int{60, 64, 72, 80, 90, 100, 110, 120} {
			for height := 24; height <= 40; height += 2 {
				name := fmt.Sprintf("swap=%t/%dx%d", swapConfigured, width, height)
				t.Run(name, func(t *testing.T) {
					model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
					metrics := testMetrics(42)
					metrics.Data.Swap = &client.Swap{}
					if swapConfigured {
						metrics.Data.Swap = &client.Swap{TotalBytes: 2 << 30, UsedBytes: 1 << 30, UsedPercent: 50}
					}
					metrics.Data.CPU.PerCoreUsagePercent = []*float64{cpuSample(25), cpuSample(75)}
					metrics.Data.Filesystems = []client.Filesystem{
						{MountPoint: "/", TotalBytes: 100, UsedBytes: 50, UsedPercent: 50},
						{MountPoint: "/data", TotalBytes: 100, UsedBytes: 90, UsedPercent: 90},
					}
					model.statuses[0].Metrics = &metrics
					model.Update(tea.WindowSizeMsg{Width: width, Height: height})
					view := model.View().Content
					if lipgloss.Width(view) > width || lipgloss.Height(view) > height {
						t.Fatalf("optional card overflow: %dx%d in %dx%d\n%s", lipgloss.Width(view), lipgloss.Height(view), width, height, ansi.Strip(view))
					}
					if !swapConfigured && width >= 60 && strings.Contains(ansi.Strip(view), "not configured") {
						t.Fatal("unconfigured swap introduced an unwanted status block")
					}
				})
			}
		}
	}
}

func TestCrowdedDashboardSeparatesSwapAndKeepsCoreLegend(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 32}, {Width: 160, Height: 50}} {
		model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
		metrics := testMetrics(26)
		metrics.Data.Swap = &client.Swap{TotalBytes: 4 << 30, UsedBytes: 58 << 20, UsedPercent: 1.4}
		for range 12 {
			metrics.Data.CPU.PerCoreUsagePercent = append(metrics.Data.CPU.PerCoreUsagePercent, cpuSample(26))
		}
		metrics.Data.Filesystems = []client.Filesystem{
			{MountPoint: "/", TotalBytes: 100, UsedBytes: 73, UsedPercent: 73},
			{MountPoint: "/boot", TotalBytes: 100, UsedBytes: 11, UsedPercent: 11},
		}
		model.statuses[0].Metrics = &metrics
		model.Update(size)
		view := model.View().Content
		if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
			t.Fatalf("crowded layout overflowed %dx%d: %dx%d", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view))
		}
		plain := strings.Split(ansi.Strip(view), "\n")
		seenSwapCard := false
		for i, row := range plain {
			if strings.Contains(row, "Swap  1.4%") {
				seenSwapCard = true
				if i == 0 || strings.TrimSpace(plain[i-1]) != "" {
					t.Fatalf("swap touches disk/uptime card at %dx%d: %q", size.Width, size.Height, plain)
				}
			}
		}
		if size.Width == 160 && !seenSwapCard {
			t.Fatal("large dashboard did not render the configured swap card")
		}
	}
}
