package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit", "config.yaml")
	want := Config{Version: 1, Servers: []Server{{Name: "local", Address: "http://127.0.0.1:8787"}}}
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Servers) != 1 || got.Servers[0] != want.Servers[0] {
		t.Fatalf("loaded config = %+v, want %+v", got, want)
	}
}

func TestSaveAndLoadSSHServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := Server{
		Name:    "remote",
		Address: "http://127.0.0.1:18787",
		SSH: &SSHConfig{
			Host:         "prod",
			User:         "orbit",
			Port:         2222,
			IdentityFile: "/home/orbit/.ssh/id_ed25519",
			RemoteHost:   "127.0.0.1",
			RemotePort:   8787,
		},
	}
	if err := Save(path, Config{Servers: []Server{want}}); err != nil {
		t.Fatalf("save SSH server: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load SSH server: %v", err)
	}
	if len(cfg.Servers) != 1 || *cfg.Servers[0].SSH != *want.SSH {
		t.Fatalf("loaded SSH settings = %+v, want %+v", cfg.Servers, want)
	}
}

func TestSaveAndLoadServiceRestartTokenPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := Server{Name: "remote", Address: "https://agent.example", ServiceRestartTokenFile: "/run/secrets/stardust-restart-token"}
	if err := Save(path, Config{Servers: []Server{want}}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 1 || got.Servers[0].ServiceRestartTokenFile != want.ServiceRestartTokenFile {
		t.Fatalf("restart token file path was not preserved: %+v", got.Servers)
	}
	if err := (Server{Name: "remote", Address: "https://agent.example", ServiceRestartTokenFile: "relative/token"}).Validate(); err == nil || !strings.Contains(err.Error(), "service_restart_token_file must be an absolute path") {
		t.Fatalf("relative restart token path was accepted: %v", err)
	}
}

func TestMissingConfigIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	if cfg.Version != 1 || len(cfg.Servers) != 0 {
		t.Fatalf("unexpected empty config: %+v", cfg)
	}
}

func TestLoadSupportsConfigWithoutVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("servers:\n  - name: local\n    address: http://127.0.0.1:8787\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	if cfg.Version != 1 || len(cfg.Servers) != 1 {
		t.Fatalf("loaded legacy config = %+v", cfg)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nservers:\n  - name: local\n    address: ftp://example.com\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "validate config") {
		t.Fatalf("load invalid config error = %v", err)
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: [\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("load malformed config error = %v", err)
	}
}

func TestServerMutations(t *testing.T) {
	cfg := Config{Version: 1}
	if err := cfg.AddServer(Server{Name: " local ", Address: " http://127.0.0.1:8787 ", Description: " local "}); err != nil {
		t.Fatalf("add server: %v", err)
	}
	if got := cfg.Servers[0]; got != (Server{Name: "local", Address: "http://127.0.0.1:8787", Description: "local"}) {
		t.Fatalf("added server = %+v", got)
	}
	if err := cfg.AddServer(Server{Name: "local", Address: "http://127.0.0.1:8788"}); err == nil {
		t.Fatal("duplicate add succeeded")
	}
	if err := cfg.UpdateServer("local", Server{Name: "production", Address: "https://example.com", Description: "production"}); err != nil {
		t.Fatalf("update server: %v", err)
	}
	if err := cfg.RemoveServer("production"); err != nil {
		t.Fatalf("remove server: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("servers after remove = %+v", cfg.Servers)
	}
	if err := cfg.RemoveServer("production"); err == nil {
		t.Fatal("removing missing server succeeded")
	}
}

func TestValidateRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name    string
		server  Server
		message string
	}{
		{name: "missing name", server: Server{Address: "http://example.com"}, message: "name must not be empty"},
		{name: "missing address", server: Server{Name: "server"}, message: "address must not be empty"},
		{name: "malformed URL", server: Server{Name: "server", Address: "http://[::1"}, message: "not a valid URL"},
		{name: "unsupported scheme", server: Server{Name: "server", Address: "ftp://example.com"}, message: "must use http:// or https://"},
		{name: "missing host", server: Server{Name: "server", Address: "http:///metrics"}, message: "must include a host"},
		{name: "credentials", server: Server{Name: "server", Address: "http://user:pass@example.com"}, message: "must not include credentials"},
		{name: "query", server: Server{Name: "server", Address: "http://example.com?token=secret"}, message: "must not include a query"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := (Config{Version: 1, Servers: []Server{test.server}}).Validate(); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("validation error = %v, want message containing %q", err, test.message)
			}
		})
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	cfg := Config{Version: 1, Servers: []Server{
		{Name: "server", Address: "http://127.0.0.1:8787"},
		{Name: " server ", Address: "http://127.0.0.1:8788"},
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate validation error = %v", err)
	}
}

func TestValidateRejectsConflictingSSHTunnelPorts(t *testing.T) {
	ssh := &SSHConfig{Host: "prod", RemotePort: 8787}
	cfg := Config{Version: 1, Servers: []Server{
		{Name: "one", Address: "http://127.0.0.1:18787", SSH: ssh},
		{Name: "two", Address: "http://127.0.0.2:018787", SSH: ssh},
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "shares local SSH tunnel port 18787") {
		t.Fatalf("conflicting tunnel port error = %v", err)
	}
}

func TestValidateSSHSettings(t *testing.T) {
	base := Server{
		Name:    "remote",
		Address: "http://127.0.0.1:18787",
		SSH:     &SSHConfig{Host: "prod", User: "orbit", RemotePort: 8787},
	}
	tests := []struct {
		name    string
		server  Server
		message string
	}{
		{name: "missing host", server: sshServer(func(ssh *SSHConfig) { ssh.Host = "" }), message: "SSH host must not be empty"},
		{name: "option-like host", server: sshServer(func(ssh *SSHConfig) { ssh.Host = "-oProxyCommand=bad" }), message: "SSH host"},
		{name: "invalid user", server: sshServer(func(ssh *SSHConfig) { ssh.User = "orbit;id" }), message: "SSH user"},
		{name: "invalid SSH port", server: sshServer(func(ssh *SSHConfig) { ssh.Port = 65536 }), message: "SSH port"},
		{name: "missing remote port", server: sshServer(func(ssh *SSHConfig) { ssh.RemotePort = 0 }), message: "SSH remote port"},
		{name: "non-loopback remote address", server: sshServer(func(ssh *SSHConfig) { ssh.RemoteHost = "10.0.0.5" }), message: "must be localhost or a loopback IP"},
		{name: "public local bind", server: sshServer(func(s *SSHConfig) {}), message: "loopback IP"},
		{name: "missing local port", server: sshServerAddress("http://127.0.0.1", func(*SSHConfig) {}), message: "include a local port"},
		{name: "local path", server: sshServerAddress("http://127.0.0.1:18787/api", func(*SSHConfig) {}), message: "must not include a path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := test.server
			if test.name == "public local bind" {
				server.Address = "http://192.0.2.10:18787"
			}
			if err := server.Validate(); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("validation error = %v, want message containing %q", err, test.message)
			}
		})
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("valid SSH settings rejected: %v", err)
	}
}

func TestSaveRejectsInvalidConfigWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("existing\n"), 0o640); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	err := Save(path, Config{Servers: []Server{{Name: "invalid", Address: "ftp://example.com"}}})
	if err == nil {
		t.Fatal("saving invalid config succeeded")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read existing config: %v", readErr)
	}
	if string(data) != "existing\n" {
		t.Fatalf("existing config was changed to %q", data)
	}
}

func TestSavePreservesExistingFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Config{Servers: []Server{{Name: "server", Address: "http://example.com"}}}); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	if err := Save(path, Config{Servers: []Server{{Name: "server", Address: "http://example.com", Description: "updated"}}}); err != nil {
		t.Fatalf("updated save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("config mode = %o, want 640", got)
	}
}

func sshServer(change func(*SSHConfig)) Server {
	return sshServerAddress("http://127.0.0.1:18787", change)
}

func sshServerAddress(address string, change func(*SSHConfig)) Server {
	ssh := &SSHConfig{Host: "prod", User: "orbit", RemotePort: 8787}
	change(ssh)
	return Server{Name: "remote", Address: address, SSH: ssh}
}
