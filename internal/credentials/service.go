// Package credentials loads secrets without storing them in Orbit's config.
package credentials

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

func ServiceToken(path string) (string, error) {
	return readServiceToken(path, "service_token_file", "service")
}

func ServiceLogToken(path string) (string, error) {
	return readServiceToken(path, "service_log_token_file", "service log")
}

func ServiceRestartToken(path string) (string, error) {
	return readServiceToken(path, "service_restart_token_file", "service restart")
}

func readServiceToken(path, setting, label string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("configure %s for this server (a private file containing its %s read token)", setting, label)
	}
	link, err := os.Lstat(path)
	if err != nil || !link.Mode().IsRegular() {
		return "", fmt.Errorf("%s must exist and be regular, not a symlink", strings.ReplaceAll(setting, "_", " "))
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open %s", strings.ReplaceAll(setting, "_", " "))
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("cannot inspect %s", strings.ReplaceAll(setting, "_", " "))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !os.SameFile(link, info) || !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || (info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600) {
		return "", fmt.Errorf("%s must be owned by the current user with mode 0400 or 0600", strings.ReplaceAll(setting, "_", " "))
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return "", fmt.Errorf("cannot read %s or file exceeds 4096 bytes", strings.ReplaceAll(setting, "_", " "))
	}
	defer clear(data)
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || len(token) > 256 || strings.IndexFunc(token, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return "", fmt.Errorf("%s token must contain 32–256 printable non-space ASCII characters", label)
	}
	return token, nil
}
