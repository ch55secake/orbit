package tui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestShellSelectionCopiesTextNotFormattingOrPadding(t *testing.T) {
	for _, test := range []struct {
		name, screen string
		anchor, end  uv.Position
		want         string
	}{
		{"one line", "\x1b[31mhello world\x1b[0m    ", uv.Position{X: 0}, uv.Position{X: 4}, "hello"},
		{"reverse", "hello world    ", uv.Position{X: 10}, uv.Position{X: 6}, "world"},
		{"multiple lines", "hello    \nworld    \n       ", uv.Position{X: 2}, uv.Position{X: 8, Y: 2}, "llo\nworld"},
		{"internal spaces", "one two    ", uv.Position{}, uv.Position{X: 6}, "one two"},
		{"wide glyph", "界abc   ", uv.Position{X: 1}, uv.Position{X: 1}, "界"},
		{"combining mark", "e\u0301abc   ", uv.Position{}, uv.Position{}, "e\u0301"},
		{"emoji", "👩‍💻abc    ", uv.Position{X: 1}, uv.Position{X: 1}, "👩‍💻"},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection := &shellSelection{screen: test.screen, anchor: test.anchor, end: test.end}
			if got := selection.content(false); got != test.want {
				t.Fatalf("copy = %q, want %q", got, test.want)
			}
			highlighted := selection.content(true)
			if ansi.Strip(highlighted) != ansi.Strip(test.screen) {
				t.Fatalf("selection changed displayed text: %q", highlighted)
			}
			originalLines, highlightedLines := strings.Split(test.screen, "\n"), strings.Split(highlighted, "\n")
			for row := range originalLines {
				if ansi.StringWidth(originalLines[row]) != ansi.StringWidth(highlightedLines[row]) {
					t.Fatal("highlight changed cell widths")
				}
			}
		})
	}
}
