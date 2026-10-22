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
