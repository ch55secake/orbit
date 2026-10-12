package shell

import (
	"fmt"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

var modifiedNavigationFormats = map[rune]string{
	uv.KeyUp:     "\x1b[1;%dA",
	uv.KeyDown:   "\x1b[1;%dB",
	uv.KeyRight:  "\x1b[1;%dC",
	uv.KeyLeft:   "\x1b[1;%dD",
	uv.KeyHome:   "\x1b[1;%dH",
	uv.KeyEnd:    "\x1b[1;%dF",
	uv.KeyInsert: "\x1b[2;%d~",
	uv.KeyDelete: "\x1b[3;%d~",
	uv.KeyPgUp:   "\x1b[5;%d~",
	uv.KeyPgDown: "\x1b[6;%d~",
}

// The pinned VT encoder drops Ctrl/Shift-modified navigation keys. Use the
// xterm modifier convention for these keys; leave mode-aware plain keys and
// text/control encoding to the emulator.
func sendKey(terminal vt.Terminal, key uv.KeyPressEvent) {
	if sequence := modifiedNavigation(key); sequence != "" {
		_, _ = io.WriteString(terminal.InputPipe(), sequence)
		return
	}
	terminal.SendKey(key)
}

func modifiedNavigation(key uv.KeyPressEvent) string {
	const supported = uv.ModShift | uv.ModAlt | uv.ModCtrl
	if key.Mod == 0 || key.Mod & ^supported != 0 {
		return ""
	}
	format, ok := modifiedNavigationFormats[key.Code]
	if !ok {
		return ""
	}
	modifier := 1
	if key.Mod&uv.ModShift != 0 {
		modifier += 1
	}
	if key.Mod&uv.ModAlt != 0 {
		modifier += 2
	}
	if key.Mod&uv.ModCtrl != 0 {
		modifier += 4
	}
	return fmt.Sprintf(format, modifier)
}
