package views

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderUsesClearHeaderAndKeyHintMenu(t *testing.T) {
	view := Render(DashboardData{
		Version:    "1.2.3",
		ConfigPath: "~/.config/orbit/config.yaml",
		Table:      "SERVER TABLE",
	})

	for _, want := range []string{
		"ORBIT",
		"SERVER DASHBOARD",
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
