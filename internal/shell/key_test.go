package shell

import (
	"io"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

func TestModifiedNavigation(t *testing.T) {
	for _, test := range []struct {
		name string
		key  uv.KeyPressEvent
		want string
	}{
		{"ctrl right", uv.KeyPressEvent{Code: uv.KeyRight, Mod: uv.ModCtrl}, "\x1b[1;5C"},
		{"ctrl left", uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModCtrl}, "\x1b[1;5D"},
		{"shift up", uv.KeyPressEvent{Code: uv.KeyUp, Mod: uv.ModShift}, "\x1b[1;2A"},
		{"alt down", uv.KeyPressEvent{Code: uv.KeyDown, Mod: uv.ModAlt}, "\x1b[1;3B"},
		{"ctrl shift home", uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModCtrl | uv.ModShift}, "\x1b[1;6H"},
		{"all end", uv.KeyPressEvent{Code: uv.KeyEnd, Mod: uv.ModCtrl | uv.ModShift | uv.ModAlt}, "\x1b[1;8F"},
		{"shift insert", uv.KeyPressEvent{Code: uv.KeyInsert, Mod: uv.ModShift}, "\x1b[2;2~"},
		{"ctrl delete", uv.KeyPressEvent{Code: uv.KeyDelete, Mod: uv.ModCtrl}, "\x1b[3;5~"},
		{"ctrl page up", uv.KeyPressEvent{Code: uv.KeyPgUp, Mod: uv.ModCtrl}, "\x1b[5;5~"},
		{"alt page down", uv.KeyPressEvent{Code: uv.KeyPgDown, Mod: uv.ModAlt}, "\x1b[6;3~"},
		{"plain arrow", uv.KeyPressEvent{Code: uv.KeyRight}, "\x1b[C"},
		{"text", uv.KeyPressEvent{Code: 'é', Text: "é"}, "é"},
		{"ctrl c", uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl}, "\x03"},
		{"alt text", uv.KeyPressEvent{Code: 'b', Mod: uv.ModAlt}, "\x1bb"},
		{"shift tab", uv.KeyPressEvent{Code: uv.KeyTab, Mod: uv.ModShift}, "\x1b[Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, application := range []bool{false, true} {
				terminal := vt.NewSafeEmulator(20, 5)
				if application {
					_, _ = terminal.Write([]byte("\x1b[?1h"))
				}
				result := make(chan []byte, 1)
				go func() { data, _ := io.ReadAll(terminal); result <- data }()
				sendKey(terminal, test.key)
				_ = terminal.InputPipe().(io.Closer).Close()
				want := test.want
				if application && test.name == "plain arrow" {
					want = "\x1bOC"
				}
				if got := string(<-result); got != want {
					t.Fatalf("application=%v: got %q, want %q", application, got, want)
				}
			}
		})
	}
}
