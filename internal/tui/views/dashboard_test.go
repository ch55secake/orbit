package views

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
)

func TestRenderUsesClearHeaderAndKeyHintMenu(t *testing.T) {
	view := Render(DashboardData{
		Version:    "1.2.3",
		ConfigPath: "~/.config/orbit/config.yaml",
		Table:      "SERVER TABLE",
		ShowHelp:   true,
	})

	for _, want := range []string{
		"ORBIT",
		"Servers",
		"v1.2.3",
		"CONFIG",
		"~/.config/orbit/config.yaml",
		"↑ k",
		"↓ j",
		"previous",
		"next",
		"refresh",
		"quit",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard is missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "//") {
		t.Fatalf("dashboard header still contains decorative slashes: %q", view)
	}
}

func TestDashboardFitsTerminalWidth(t *testing.T) {
	for _, width := range []int{24, 40, 60, 80, 120} {
		for _, state := range []string{"online", "offline", "password"} {
			t.Run(fmt.Sprintf("%s/%d", state, width), func(t *testing.T) {
				status := server.Status{
					Server: config.Server{Name: "a-very-long-server-name-for-production"},
					Metrics: &client.MetricsResponse{Data: client.MetricsData{
						Hostname: "long-hostname.example.test",
						CPU:      client.CPU{UsagePercent: 42},
					}},
				}
				if state == "offline" {
					status.Error = errors.New("connection refused at http://a-long-hostname.example.test:8787/metrics")
				}
				view := Render(DashboardData{
					Width: width, Version: "dev", ConfigPath: "/a/very/long/configuration/path/config.yaml",
					Table: "SERVER  STATUS", Selected: &status, Statuses: []server.Status{status},
					PasswordPrompt: state == "password", PasswordServer: status.Server.Name,
					PasswordInput: strings.Repeat("•", 48),
				})
				if got := lipgloss.Width(view); got > width {
					t.Fatalf("dashboard width = %d, terminal = %d:\n%s", got, width, view)
				}
			})
		}
	}
}

func TestFleetSummaryCountsAndRefreshRetainsDetails(t *testing.T) {
	statuses := []server.Status{
		{Server: config.Server{Name: "alpha"}, Metrics: &client.MetricsResponse{Data: client.MetricsData{CPU: client.CPU{UsagePercent: 42}}}},
		{Error: errors.New("offline")},
		{},
	}
	summary := fleetSummary(DashboardData{Statuses: statuses})
	for _, want := range []string{"3 servers", "1 online", "1 offline", "1 pending"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q: %q", want, summary)
		}
	}
	view := Render(DashboardData{Loading: true, Selected: &statuses[0], Statuses: statuses})
	for _, want := range []string{"Loading metrics…", "alpha", "42.0%"} {
		if !strings.Contains(view, want) {
			t.Errorf("refresh view missing %q: %q", want, view)
		}
	}
}

func TestPanelsHaveConsistentWidth(t *testing.T) {
	for _, content := range []string{"short", strings.Repeat("long text ", 20)} {
		if width := lipgloss.Width(renderPanel(content, 80)); width != 80 {
			t.Errorf("panel width = %d, want 80", width)
		}
	}
}

func TestRenderMenuFitsNarrowTerminal(t *testing.T) {
	menu := renderMenu(DashboardData{Width: 42})
	if width := lipgloss.Width(menu); width > 42 {
		t.Fatalf("compact menu width = %d, want <= 42: %q", width, menu)
	}
	for _, want := range []string{"up", "down", "refresh", "quit"} {
		if !strings.Contains(menu, want) {
			t.Errorf("compact menu is missing %q: %q", want, menu)
		}
	}
}

func TestPasswordPromptShowsContextualMenu(t *testing.T) {
	menu := renderMenu(DashboardData{PasswordPrompt: true})
	for _, want := range []string{"enter", "submit", "esc", "cancel", "ctrl+c", "quit"} {
		if !strings.Contains(menu, want) {
			t.Errorf("password menu is missing %q: %q", want, menu)
		}
	}
	if strings.Contains(menu, "refresh") || strings.Contains(menu, "previous") {
		t.Fatalf("dashboard controls should be hidden during password input: %q", menu)
	}
}

func TestMenuReflectsAvailableActions(t *testing.T) {
	tests := []struct {
		name string
		data DashboardData
		want []string
		omit []string
	}{
		{
			name: "loading",
			data: DashboardData{Loading: true},
			want: []string{"previous", "next", "quit"},
			omit: []string{"refresh"},
		},
		{
			name: "empty configuration",
			data: DashboardData{Empty: true},
			want: []string{"quit"},
			omit: []string{"previous", "next", "refresh"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			menu := renderMenu(test.data)
			for _, want := range test.want {
				if !strings.Contains(menu, want) {
					t.Errorf("menu missing %q: %q", want, menu)
				}
			}
			for _, omit := range test.omit {
				if strings.Contains(menu, omit) {
					t.Errorf("menu unexpectedly contains %q: %q", omit, menu)
				}
			}
		})
	}
}
