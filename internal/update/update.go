package update

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/sshclient"
)

//go:embed remote.sh
var script string

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func Command(ctx context.Context, target config.Server, version string) (*exec.Cmd, error) {
	if version != "latest" && !versionPattern.MatchString(version) {
		return nil, fmt.Errorf("version must be latest or vX.Y.Z")
	}
	if target.SSH == nil {
		return nil, fmt.Errorf("server %q has no SSH settings", target.Name)
	}
	if target.SSH.RemotePort < 1 || target.SSH.RemotePort > 65535 {
		return nil, fmt.Errorf("valid SSH remote Stardust port required")
	}
	host := target.SSH.RemoteHost
	if host == "" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("updates require Stardust on the SSH host's loopback interface")
	}
	health := (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(target.SSH.RemotePort)), Path: "/api/v1/health"}).String()
	invocation := "bash -c " + sshclient.Quote(script) + " orbit-update " + sshclient.Quote(version) + " " + sshclient.Quote(health)
	// Elevate the entire script, so downloads, staging, and backups are private
	// root-owned files. sudo/OpenSSH read any password from the terminal.
	remote := "if [ \"$(id -u)\" = 0 ]; then " + invocation + "; else sudo -- " + invocation + "; fi"
	return sshclient.Command(ctx, target, true, strings.TrimSpace(remote))
}
