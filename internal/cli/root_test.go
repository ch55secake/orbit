package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
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

func TestServerCommandsConfigureAndDisableSSHTunnel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := execute(t, []string{
		"--config", path, "servers", "add",
		"--name", "remote", "--address", "http://127.0.0.1:18787",
		"--ssh-host", "prod", "--ssh-user", "orbit", "--ssh-port", "2222",
		"--ssh-identity-file", "/keys/orbit", "--ssh-remote-host", "127.0.0.1", "--ssh-remote-port", "8787",
	}); err != nil {
		t.Fatalf("add SSH server: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Servers[0].SSH; got == nil || got.Host != "prod" || got.User != "orbit" || got.Port != 2222 || got.IdentityFile != "/keys/orbit" || got.RemotePort != 8787 {
		t.Fatalf("configured SSH settings = %+v", got)
	}
	listOutput, err := execute(t, []string{"--config", path, "servers", "list"})
	if err != nil || !strings.Contains(listOutput, "[SSH via prod]") {
		t.Fatalf("SSH server list output = %q, err = %v", listOutput, err)
	}

	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "remote", "--ssh-user", "deploy"}); err != nil {
		t.Fatalf("edit SSH user: %v", err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("reload edited config: %v", err)
	}
	if cfg.Servers[0].SSH.User != "deploy" || cfg.Servers[0].SSH.Host != "prod" {
		t.Fatalf("edited SSH settings = %+v", cfg.Servers[0].SSH)
	}

	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "remote", "--direct"}); err != nil {
		t.Fatalf("disable SSH tunnel: %v", err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("reload direct config: %v", err)
	}
	if cfg.Servers[0].SSH != nil {
		t.Fatalf("SSH config remains after --direct: %+v", cfg.Servers[0].SSH)
	}
}

func TestPrintStatusReportsTunnelStateAndErrors(t *testing.T) {
	target := config.Server{
		Name:    "remote",
		Address: "http://127.0.0.1:18787",
		SSH:     &config.SSHConfig{Host: "prod", RemotePort: 8787},
	}
	t.Run("connected", func(t *testing.T) {
		var output bytes.Buffer
		api := &statusMetricsClient{response: client.MetricsResponse{Data: client.MetricsData{CPU: client.CPU{UsagePercent: 12}}}}
		if err := printStatus(context.Background(), &output, config.Config{Servers: []config.Server{target}}, api, &statusTunnelManager{}, nil); err != nil {
			t.Fatalf("print status: %v", err)
		}
		for _, expected := range []string{"tunnel: connected", "status: online", "CPU: 12.0%"} {
			if !strings.Contains(output.String(), expected) {
				t.Errorf("status output %q does not contain %q", output.String(), expected)
			}
		}
	})

	t.Run("startup error", func(t *testing.T) {
		var output bytes.Buffer
		api := &statusMetricsClient{}
		tunnels := &statusTunnelManager{ensureErr: errors.New("SSH key rejected")}
		if err := printStatus(context.Background(), &output, config.Config{Servers: []config.Server{target}}, api, tunnels, nil); err != nil {
			t.Fatalf("print status: %v", err)
		}
		for _, expected := range []string{"tunnel: disconnected", "status: offline", "SSH key rejected"} {
			if !strings.Contains(output.String(), expected) {
				t.Errorf("status output %q does not contain %q", output.String(), expected)
			}
		}
		if api.calls != 0 {
			t.Fatalf("metrics calls = %d, want no request after tunnel startup fails", api.calls)
		}
	})
}

func TestPromptSSHPasswordRejectsNonInteractiveInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close test input reader: %v", err)
		}
	}()
	defer func() {
		if err := writer.Close(); err != nil {
			t.Errorf("close test input writer: %v", err)
		}
	}()
	previousStdin := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = previousStdin }()

	target := config.Server{SSH: &config.SSHConfig{Host: "production", User: "orbit"}}
	_, err = promptSSHPassword(context.Background(), target)
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("non-interactive prompt error = %v", err)
	}
}

type statusMetricsClient struct {
	response client.MetricsResponse
	err      error
	calls    int
}

func (f *statusMetricsClient) Metrics(context.Context, string) (client.MetricsResponse, error) {
	f.calls++
	return f.response, f.err
}

type statusTunnelManager struct {
	ensureErr error
}

func (f *statusTunnelManager) Ensure(context.Context, config.Server) error {
	return f.ensureErr
}

func (*statusTunnelManager) EnsureWithPassword(context.Context, config.Server, []byte) error {
	return nil
}

func (*statusTunnelManager) Reconnect(context.Context, config.Server) error { return nil }

var _ server.TunnelManager = (*statusTunnelManager)(nil)

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
