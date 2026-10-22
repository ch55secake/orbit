package components

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

var sgrSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Terminal SGR resets used to expose holes in colored card labels and tiles:
// visual-width tests cannot detect that the terminal default background leaked.
func assertCardTextBackground(t *testing.T, row string) {
	t.Helper()
	background := false
	column := 0
	lastContent := utf8.RuneCountInString(strings.TrimRight(ansi.Strip(row), " "))
	for len(row) > 0 {
		if match := sgrSequence.FindStringIndex(row); match != nil && match[0] == 0 {
			sequence := row[:match[1]]
			if strings.Contains(sequence, "[m") || strings.Contains(sequence, "[0m") || strings.Contains(sequence, ";49m") {
				background = false
			}
			if strings.Contains(sequence, "48;2;24;30;41") {
				background = true
			}
			row = row[match[1]:]
			continue
		}
		character, size := utf8.DecodeRuneInString(row)
		if column > 0 && column < lastContent && !background {
			t.Fatalf("%q rendered on terminal background in %q", character, ansi.Strip(row))
		}
		column++
		row = row[size:]
	}
}
