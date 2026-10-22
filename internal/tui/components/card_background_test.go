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
	if !cardTextHasBackground(row) {
		t.Fatalf("card text leaked onto the terminal background: %q", ansi.Strip(row))
	}
}

func cardTextHasBackground(row string) bool {
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
		_, size := utf8.DecodeRuneInString(row)
		if column > 0 && column < lastContent && !background {
			return false
		}
		column++
		row = row[size:]
	}
	return true
}

func TestCardTextBackgroundCheckDetectsResetLeak(t *testing.T) {
	background := "\x1b[48;2;24;30;41m"
	foreground := "\x1b[38;2;148;163;184;48;2;24;30;41m"
	good := background + " " + foreground + "x" + background + " "
	bad := background + " " + foreground + "x\x1b[0m" + "y" + background + " "
	if !cardTextHasBackground(good) {
		t.Fatal("background checker rejected a properly styled row")
	}
	if cardTextHasBackground(bad) {
		t.Fatal("background checker missed text after an SGR reset")
	}
}
