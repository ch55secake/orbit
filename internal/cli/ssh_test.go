package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/config"
)

func TestSSHCommandRunsOpenSSHWithConfiguredTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "config.yaml")
	if err := config.Save(path, config.Config{Version: 1, Servers: []config.Server{{Name: "mele", Address: "http://127.0.0.1:18787", SSH: &config.SSHConfig{Host: "prod", User: "root", RemotePort: 8787}}}}); err != nil {
		t.Fatal(err)
	}
	cmd := New("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", path, "ssh", "mele"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-t\n") || !strings.Contains(out.String(), "--\nprod\n") {
		t.Fatalf("unexpected SSH args: %q", out.String())
	}
	cmd = New("test")
	cmd.SetArgs([]string{"--config", path, "ssh", "missing"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("unknown server accepted")
	}
}

func TestSSHCommandNormalizesHandEditedConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "config.yaml")
	data := `version: 1
servers:
  - name: " mele "
    address: " http://127.0.0.1:18787 "
    ssh:
      host: " prod "
      user: " root "
      identity_file: " /keys/key with spaces "
      remote_port: 8787
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := New("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", path, "ssh", "mele"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, argument := range []string{"-l\nroot\n", "-i\n/keys/key with spaces\n", "--\nprod\n"} {
		if !strings.Contains(out.String(), argument) {
			t.Fatalf("missing normalized argument %q in %q", argument, out.String())
		}
	}
}
