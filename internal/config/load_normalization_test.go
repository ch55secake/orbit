package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadNormalizesServerSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := `version: 1
servers:
  - name: " prod "
    address: " http://127.0.0.1:18787 "
    description: " primary  server "
    ssh:
      host: " prod-alias "
      user: " orbit "
      identity_file: " ~/.ssh/key with spaces "
      remote_host: " localhost "
      remote_port: 8787
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Server{Name: "prod", Address: "http://127.0.0.1:18787", Description: "primary  server", SSH: &SSHConfig{Host: "prod-alias", User: "orbit", IdentityFile: "~/.ssh/key with spaces", RemoteHost: "localhost", RemotePort: 8787}}
	if len(cfg.Servers) != 1 || !reflect.DeepEqual(cfg.Servers[0], want) {
		t.Fatalf("Load retained whitespace: got %+v / %+v; want %+v", cfg.Servers[0], cfg.Servers[0].SSH, want)
	}
}

func TestLoadNormalizationRetainsValidation(t *testing.T) {
	for _, test := range []struct{ name, servers, want string }{
		{"duplicate names", `  - name: " prod "
    address: " http://127.0.0.1:8787 "
  - name: prod
    address: http://127.0.0.1:8788
`, "duplicated"},
		{"empty name", `  - name: "   "
    address: http://127.0.0.1:8787
`, "name must not be empty"},
		{"internal control", `  - name: "pro\nd"
    address: http://127.0.0.1:8787
`, "control character"},
		{"invalid SSH host", `  - name: prod
    address: http://127.0.0.1:8787
    ssh:
      host: " -prod "
      remote_port: 8787
`, "SSH host"},
		{"duplicate tunnel ports", `  - name: " prod "
    address: " http://127.0.0.1:8787 "
    ssh:
      host: " prod "
      remote_port: 8787
  - name: other
    address: http://127.0.0.1:8787
    ssh:
      host: other
      remote_port: 8787
`, "shares local SSH tunnel port"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("version: 1\nservers:\n"+test.servers), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}
