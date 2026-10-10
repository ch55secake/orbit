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
