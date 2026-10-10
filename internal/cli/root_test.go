package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	commandOutput, err := execute(t, []string{
		"--config", path,
		"servers", "add",
		"--name", "server-01",
		"--address", "http://127.0.0.1:8787",
		"--description", "initial",
	})
	if err != nil {
		t.Fatalf("add server: %v", err)
	}
	if commandOutput != "" {
		t.Fatalf("add output = %q, want empty output", commandOutput)
	}

	if _, err := execute(t, []string{
		"--config", path,
		"servers", "edit",
		"--name", "server-01",
		"--new-name", "server-prod",
		"--address", "https://example.com",
		"--description", "updated",
	}); err != nil {
		t.Fatalf("edit server: %v", err)
	}

	output, err := execute(t, []string{"--config", path, "servers", "list"})
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	want := "server-prod\thttps://example.com — updated\n"
	if output != want {
		t.Fatalf("list output = %q, want %q", output, want)
	}

	if _, err := execute(t, []string{
		"--config", path,
		"servers", "remove",
		"--name", "server-prod",
	}); err != nil {
		t.Fatalf("remove server: %v", err)
	}

	output, err = execute(t, []string{"--config", path, "servers", "list"})
	if err != nil {
		t.Fatalf("list empty servers: %v", err)
	}
	if output != "No servers configured.\n" {
		t.Fatalf("empty list output = %q", output)
	}
}

func TestAddServerRejectsInvalidAddressBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_, err := execute(t, []string{
		"--config", path,
		"servers", "add",
		"--name", "server-01",
		"--address", "ftp://example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "must use http:// or https://") {
		t.Fatalf("add invalid address error = %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid add config stat error = %v, want file to be absent", statErr)
	}
}

func execute(t *testing.T, args []string) (string, error) {
	t.Helper()
	command := New("test")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}
