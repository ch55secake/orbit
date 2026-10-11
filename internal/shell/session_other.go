//go:build !linux && !darwin

package shell

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"os/exec"
)

type Session struct{}

func Start(*exec.Cmd, int, int) (*Session, error) {
	return nil, fmt.Errorf("embedded SSH requires Linux or macOS; use orbit ssh instead")
}
func (*Session) Events() <-chan struct{}       { return nil }
func (*Session) Render() string                { return "" }
func (*Session) Cursor() (uv.Position, bool)   { return uv.Position{}, false }
func (*Session) Result() (bool, error)         { return true, nil }
func (*Session) SendKey(uv.KeyPressEvent) bool { return false }
func (*Session) Paste(string) bool             { return false }
func (*Session) Resize(int, int) error         { return nil }
func (*Session) Close()                        {}
