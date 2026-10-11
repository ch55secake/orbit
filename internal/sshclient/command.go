package sshclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ch55secake/orbit/internal/config"
)

// Command uses OpenSSH directly, never a local shell. OpenSSH owns terminal
// authentication, host-key checks, and interactive password handling.
func Command(ctx context.Context, target config.Server, tty bool, remote string) (*exec.Cmd, error) {
	if target.SSH == nil {
		return nil, fmt.Errorf("server %q has no SSH settings; configure --ssh-host first", target.Name)
	}
	s := target.SSH
	args := []string{"-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "RemoteCommand=none"}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	if s.Port > 0 {
		args = append(args, "-p", strconv.Itoa(s.Port))
	}
	if s.User != "" {
		args = append(args, "-l", s.User)
	}
	if s.IdentityFile != "" {
		identity := s.IdentityFile
		if strings.HasPrefix(identity, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("resolve identity: %w", err)
			}
			identity = filepath.Join(home, identity[2:])
		}
		args = append(args, "-i", identity)
	}
	args = append(args, "--", s.Host)
	if remote != "" {
		args = append(args, remote)
	}
	return exec.CommandContext(ctx, "ssh", args...), nil
}

func Find(cfg config.Config, name string) (config.Server, error) {
	for _, target := range cfg.Servers {
		if target.Name == name {
			return target, nil
		}
	}
	return config.Server{}, fmt.Errorf("server %q not found", name)
}

// Quote encodes one argument for the remote POSIX shell used by OpenSSH.
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
