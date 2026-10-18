package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

const cliServiceToken = "0123456789abcdef0123456789abcdef"

func serviceCLIConfig(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	fixture, err := os.ReadFile("../client/testdata/stardust/v1/services-states.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != "GET" {
			t.Error("mutation attempted")
		}
		var body string
		if request.URL.Path == "/api/v1/capabilities" {
			body = `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer"}}`
		} else {
			if request.Header.Get("Authorization") != "Bearer "+cliServiceToken {
				t.Error("wrong token")
			}
			if request.URL.Path == "/api/v1/services" {
				body = string(fixture)
				if request.URL.Query().Get("filter") != "worker & café" || request.URL.Query().Get("limit") != "10" {
					t.Error("CLI filters lost")
				}
			} else {
				body = `{"api_version":"v1","data":{"name":"active.service","id":"canonical.service","description":"\u001b[31mworker\u001b[0m\nnext","load_state":"loaded","active_state":"active","sub_state":"running","unit_file_state":"enabled","main_pid":456,"main_process":{"name":"worker","effective_uid":1001}}}`
			}
		}
		out.Header().Set("Content-Type", "application/json")
		if _, err := out.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(cliServiceToken), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, config.Config{Servers: []config.Server{{Name: "local", Address: server.URL, ServiceTokenFile: tokenPath}}}); err != nil {
		t.Fatal(err)
	}
	return path, &requests
}

func TestServicesCLIListsAndInspectsWithoutShell(t *testing.T) {
	path, requests := serviceCLIConfig(t)
	output, err := execute(t, []string{"--config", path, "services", "list", "local", "--filter", "worker & café", "--limit", "10"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"active.service", "active/running", "failed/failed", "inactive/dead", "missing.service", "PID 123"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %s", want, output)
		}
	}
	output, err = execute(t, []string{"--config", path, "services", "show", "local", "active.service"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Canonical ID: canonical.service", "Description: worker next", "Load: loaded", "State: active / running", "Unit file: enabled", "Main PID: 456", "Process: worker (effective UID 1001)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %s", want, output)
		}
	}
	if requests.Load() != 4 || strings.Contains(output, cliServiceToken) || strings.Contains(output, "\x1b") {
		t.Fatalf("bad request count or unsafe output: %d %q", requests.Load(), output)
	}
}

func TestServiceTokenPathConfigurationSurvivesEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	tokenPath := filepath.Join(t.TempDir(), "token")
	for _, args := range [][]string{
		{"servers", "add", "--name", "local", "--address", "http://127.0.0.1:8787", "--service-token-file", tokenPath},
		{"servers", "edit", "--name", "local", "--description", "kept"},
	} {
		if _, err := execute(t, append([]string{"--config", path}, args...)); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Servers[0].ServiceTokenFile != tokenPath {
		t.Fatal("edit discarded credential reference")
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--address", "http://127.0.0.1:8788"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Servers[0].ServiceTokenFile != "" {
		t.Fatal("address change retained a credential for another server")
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--service-token-file", tokenPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--service-token-file", ""}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), cliServiceToken) {
		t.Fatal("credential saved in config")
	}
}

type serviceFailWriter struct{}

func (serviceFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestServiceDetailZeroPIDAndWriterFailures(t *testing.T) {
	var output strings.Builder
	if err := printServiceDetail(&output, client.ServiceUnit{Name: "inactive.service"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Main PID: none") {
		t.Fatal("zero PID shown as real process")
	}
	if err := printServiceDetail(serviceFailWriter{}, client.ServiceUnit{}); err == nil {
		t.Fatal("writer error lost")
	}
}

func TestServiceCLIRejectsInvalidArgumentsBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{{"services", "list", "local", "--limit", "0"}, {"services", "list", "local", "--limit", "51"}, {"services", "show", "local", "*.service"}} {
		if _, err := execute(t, append([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, args...)); err == nil || strings.Contains(err.Error(), "not found") {
			t.Fatalf("invalid input reached server lookup: %v", err)
		}
	}
}
