package shell

import (
	"fmt"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

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
	var final byte
	switch key.Code {
	case uv.KeyUp:
		final = 'A'
	case uv.KeyDown:
		final = 'B'
	case uv.KeyRight:
		final = 'C'
	case uv.KeyLeft:
		final = 'D'
	case uv.KeyHome:
		final = 'H'
	case uv.KeyEnd:
		final = 'F'
	}
	if final != 0 {
		return fmt.Sprintf("\x1b[1;%d%c", modifier, final)
	}
	var number int
	switch key.Code {
	case uv.KeyInsert:
		number = 2
	case uv.KeyDelete:
		number = 3
	case uv.KeyPgUp:
		number = 5
	case uv.KeyPgDown:
		number = 6
	}
	if number != 0 {
		return fmt.Sprintf("\x1b[%d;%d~", number, modifier)
	}
	return ""
}
