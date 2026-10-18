// Package credentials loads secrets without storing them in Orbit's config.
package credentials

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

func ServiceToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("configure service_token_file for this server (a private file containing its service read token)")
	}
	link, err := os.Lstat(path)
	if err != nil || !link.Mode().IsRegular() {
		return "", errors.New("service token file must exist and be regular, not a symlink")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", errors.New("cannot open service token file")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", errors.New("cannot inspect service token file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !os.SameFile(link, info) || !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || (info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600) {
		return "", errors.New("service token file must be owned by the current user with mode 0400 or 0600")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return "", errors.New("cannot read service token file or file exceeds 4096 bytes")
	}
	defer clear(data)
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || len(token) > 256 || strings.IndexFunc(token, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return "", errors.New("service token must contain 32–256 printable non-space ASCII characters")
	}
	return token, nil
}
