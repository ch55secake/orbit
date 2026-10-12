package views

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestShortTerminalShowsFittingShellWarning(t *testing.T) {
	data := DashboardData{Width: 80, Height: 12, Table: "fleet\nalpha\nbeta", ShellActive: true, ShellServer: "alpha", ShellContent: "shell"}
	if TerminalBounds(data).Fits() {
		t.Fatal("80x12 should not fit the frame")
	}
	if view := Render(data); lipgloss.Width(view) > 80 || lipgloss.Height(view) > 12 {
		t.Fatalf("short shell warning overflows: %q", view)
	}
}
