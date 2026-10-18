package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/tui/views"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Freeze the displayed screen while selecting so output cannot change what is
// highlighted or copied. The PTY continues running; clearing restores live output.
type shellSelection struct {
	screen      string
	anchor, end uv.Position
	dragging    bool
}

func (m *app) selectShell(msg tea.MouseMsg) {
	if m.shellSession == nil {
		return
	}
	bounds := m.shellBounds
	if !views.TerminalBounds(m.dashboardData()).Fits() {
		m.shellSelection = nil
		return
	}
	mouse := msg.Mouse()
	inside := mouse.X >= bounds.X && mouse.X < bounds.X+bounds.Width && mouse.Y >= bounds.Y && mouse.Y < bounds.Y+bounds.Height
	position := uv.Position{X: min(max(mouse.X-bounds.X, 0), bounds.Width-1), Y: min(max(mouse.Y-bounds.Y, 0), bounds.Height-1)}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return
		}
		m.shellSelection = nil
		if !inside {
			return
		}
		screen := m.shellSession.Render()
		lines := strings.Split(ansi.Strip(screen), "\n")
		if position.Y >= len(lines) || position.X >= ansi.StringWidth(strings.TrimRight(lines[position.Y], " ")) {
			return
		}
		m.shellSelection = &shellSelection{screen: screen, anchor: position, end: position, dragging: true}
	case tea.MouseMotionMsg:
		if m.shellSelection != nil && m.shellSelection.dragging && mouse.Button == tea.MouseLeft {
			m.shellSelection.end = position
		}
	case tea.MouseReleaseMsg:
		if m.shellSelection != nil && m.shellSelection.dragging && (mouse.Button == tea.MouseLeft || mouse.Button == tea.MouseNone) {
			m.shellSelection.end = position
			m.shellSelection.dragging = false
		}
	}
}

func (selection *shellSelection) ranges() (uv.Position, uv.Position) {
	start, end := selection.anchor, selection.end
	if start.Y > end.Y || start.Y == end.Y && start.X > end.X {
		start, end = end, start
	}
	return start, end
}

// Expand cell boundaries to whole grapheme clusters (including wide characters)
// and exclude trailing terminal padding from both highlights and clipboard text.
func selectionColumns(line string, left, right int) (int, int) {
	plain := strings.TrimRight(ansi.Strip(line), " ")
	width := ansi.StringWidth(plain)
	left, right = min(left, width), min(right, width)
	column := 0
	for len(plain) > 0 {
		cluster, size := ansi.FirstGraphemeCluster(plain, ansi.GraphemeWidth)
		next := column + size
		if left > column && left < next {
			left = column
		}
		if right > column && right < next {
			right = next
		}
		column = next
		plain = plain[len(cluster):]
	}
	return left, right
}

func (selection *shellSelection) content(highlight bool) string {
	start, end := selection.ranges()
	lines := strings.Split(selection.screen, "\n")
	selected := make([]string, 0, end.Y-start.Y+1)
	for row, line := range lines {
		if row < start.Y || row > end.Y {
			continue
		}
		left, right := 0, ansi.StringWidth(line)
		if row == start.Y {
			left = start.X
		}
		if row == end.Y {
			right = end.X + 1
		}
		left, right = selectionColumns(line, left, right)
		text := ansi.Cut(ansi.Strip(line), left, right)
		if highlight {
			lines[row] = ansi.Cut(line, 0, left) + lipgloss.NewStyle().Reverse(true).Render(text) + ansi.Cut(line, right, ansi.StringWidth(line))
		} else {
			selected = append(selected, text)
		}
	}
	if highlight {
		return strings.Join(lines, "\n")
	}
	return strings.TrimRight(strings.Join(selected, "\n"), "\n")
}
