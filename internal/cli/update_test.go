package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/config"
)

func TestRemoteUpdateRequiresExplicitConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\ntouch \"$ORBIT_TEST_MARKER\"\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORBIT_TEST_MARKER", marker)
	if err := config.Save(path, config.Config{Version: 1, Servers: []config.Server{{Name: "mele", Address: "http://127.0.0.1:18787", SSH: &config.SSHConfig{Host: "prod", RemotePort: 18787}}}}); err != nil {
		t.Fatal(err)
	}
	cmd := New("test")
	cmd.SetArgs([]string{"--config", path, "stardust", "update", "mele"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("SSH invoked without confirmation")
	}
	cmd = New("test")
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--config", path, "stardust", "update", "mele", "--version", "v1.2.3", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("confirmed update did not invoke SSH")
	}
	if !strings.Contains(output.String(), "v1.2.3") || !strings.Contains(output.String(), "18787/api/v1/health") {
		t.Fatal("selected version/port missing from update")
	}
}
