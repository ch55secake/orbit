package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

const cliServiceToken = "0123456789abcdef0123456789abcdef"
const cliServiceLogToken = "abcdef0123456789abcdef0123456789"
const cliServiceRestartToken = "fedcba9876543210fedcba9876543210"

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

func serviceRestartCLIConfig(t *testing.T, restartEnabled, unitAllowed bool, finalStates ...string) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var posts, statuses atomic.Int32
	finalState, jobResult, errorCode := client.RestartSucceeded, "succeeded", ""
	if len(finalStates) > 0 {
		finalState = finalStates[0]
		jobResult = finalState
		if finalState == client.RestartFailed {
			errorCode = "restart_failed"
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		out.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/capabilities":
			if request.Header.Get("Authorization") != "" {
				t.Error("capability discovery carried a credential")
			}
			_, _ = fmt.Fprintf(out, `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","restart":%t,"restart_authentication":"bearer"}}`, restartEnabled)
		case "/api/v1/services/worker.service":
			if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+cliServiceToken {
				t.Errorf("restart preflight used wrong read credential: %s %q", request.Method, request.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprintf(out, `{"api_version":"v1","data":{"name":"worker.service","id":"worker.service","active_state":"active","sub_state":"running","restart_allowed":%t}}`, unitAllowed)
		case "/api/v1/services/worker.service/restart":
			posts.Add(1)
			if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer "+cliServiceRestartToken || request.Header.Get("Idempotency-Key") == "" || request.ContentLength != 0 || request.Header.Get("Origin") != "" {
				t.Errorf("unsafe restart request: %s authorization=%q key=%q length=%d origin=%q", request.Method, request.Header.Get("Authorization"), request.Header.Get("Idempotency-Key"), request.ContentLength, request.Header.Get("Origin"))
			}
			out.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(out, `{"api_version":"v1","data":{"operation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","unit":"worker.service","state":"running","requested_at":"2026-10-18T12:00:00Z"}}`)
		case "/api/v1/service-operations/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":
			statuses.Add(1)
			if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+cliServiceRestartToken {
				t.Errorf("operation status used wrong restart credential: %s %q", request.Method, request.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprintf(out, `{"api_version":"v1","data":{"operation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","unit":"worker.service","state":%q,"requested_at":"2026-10-18T12:00:00Z","completed_at":"2026-10-18T12:00:02Z","job_result":%q,"error_code":%q,"active_state":"active","sub_state":"running"}}`, finalState, jobResult, errorCode)
		default:
			t.Errorf("unexpected restart workflow request: %s %s", request.Method, request.URL.Path)
			http.NotFound(out, request)
		}
	}))
	t.Cleanup(server.Close)
	readPath := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(readPath, []byte(cliServiceToken), 0600); err != nil {
		t.Fatal(err)
	}
	restartPath := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(restartPath, []byte(cliServiceRestartToken), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{Servers: []config.Server{{Name: "local", Address: server.URL, ServiceTokenFile: readPath, ServiceRestartTokenFile: restartPath}}}); err != nil {
		t.Fatal(err)
	}
	return configPath, &posts, &statuses
}

func TestServiceRestartCLIAuthorizationConfirmationAndReconciliation(t *testing.T) {
	path, posts, statuses := serviceRestartCLIConfig(t, true, true)
	if _, err := execute(t, []string{"--config", path, "services", "restart", "local", "worker.service"}); err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("noninteractive restart did not require explicit confirmation: %v", err)
	}
	if posts.Load() != 0 {
		t.Fatal("noninteractive cancellation sent a restart request")
	}
	output, err := execute(t, []string{"--config", path, "services", "restart", "local", "worker.service", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Restart operation", "succeeded", "not an application health check", "Current systemd state", "active / running"} {
		if !strings.Contains(output, want) {
			t.Fatalf("restart output missing %q:\n%s", want, output)
		}
	}
	if posts.Load() != 1 || statuses.Load() != 1 || strings.Contains(output, cliServiceRestartToken) || strings.Contains(output, cliServiceToken) {
		t.Fatalf("restart request/reconciliation duplicated or leaked credentials: posts=%d statuses=%d output=%q", posts.Load(), statuses.Load(), output)
	}
	output, err = execute(t, []string{"--config", path, "services", "operation", "local", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err != nil || !strings.Contains(output, "succeeded") || !strings.Contains(output, "not an application health check") {
		t.Fatalf("read-only operation inspection failed: %q %v", output, err)
	}
	if posts.Load() != 1 || statuses.Load() != 2 {
		t.Fatalf("operation inspection issued a mutation or missed status read: posts=%d statuses=%d", posts.Load(), statuses.Load())
	}
}

func TestServiceRestartCLINeverPostsWhenCanceledOrUnauthorized(t *testing.T) {
	if err := confirmServiceRestart(strings.NewReader("no\n"), &strings.Builder{}, "local", "worker.service", false, true); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("typed cancellation was not honored: %v", err)
	}
	if err := confirmServiceRestart(strings.NewReader("restart local worker.service\n"), &strings.Builder{}, "local", "worker.service", false, true); err != nil {
		t.Fatalf("exact server/unit confirmation rejected: %v", err)
	}
	if err := confirmServiceRestart(strings.NewReader("restart local other.service\n"), &strings.Builder{}, "local", "worker.service", false, true); err == nil {
		t.Fatal("confirmation for a different unit was accepted")
	}

	for _, test := range []struct {
		name           string
		restartEnabled bool
		unitAllowed    bool
		want           string
	}{
		{"restart disabled", false, true, "unavailable"},
		{"unit read-only", true, false, "allowlist"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, posts, _ := serviceRestartCLIConfig(t, test.restartEnabled, test.unitAllowed)
			_, err := execute(t, []string{"--config", path, "services", "restart", "local", "worker.service", "--yes"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unauthorized restart preflight error = %v", err)
			}
			if posts.Load() != 0 {
				t.Fatalf("preflight failure sent %d restart requests", posts.Load())
			}
		})
	}
}

func TestServiceRestartCLIReportsFailedSystemdJobAndRefreshesState(t *testing.T) {
	path, posts, statuses := serviceRestartCLIConfig(t, true, true, client.RestartFailed)
	output, err := execute(t, []string{"--config", path, "services", "restart", "local", "worker.service", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "restart job failed") {
		t.Fatalf("failed systemd job did not return an actionable error: %q %v", output, err)
	}
	for _, want := range []string{"failed", "Systemd reported a failed restart job", "Current systemd state"} {
		if !strings.Contains(output, want) {
			t.Fatalf("failed restart output missing %q:\n%s", want, output)
		}
	}
	if posts.Load() != 1 || statuses.Load() != 1 {
		t.Fatalf("failed operation was retried or not reconciled: posts=%d statuses=%d", posts.Load(), statuses.Load())
	}
}

func TestWaitServiceRestartRetriesOnlyStatusLookupAndReportsUnknown(t *testing.T) {
	operation := client.ServiceRestartOperation{ID: "0123456789abcdef0123456789abcdef", Unit: "worker.service", State: client.RestartRunning}
	var calls atomic.Int32
	var queriedID string
	result, err := waitServiceRestart(context.Background(), operation, func(_ context.Context, id string) (client.ServiceRestartOperation, error) {
		queriedID = id
		if calls.Add(1) == 1 {
			return client.ServiceRestartOperation{}, &client.ServiceError{Code: "restart_transport"}
		}
		return client.ServiceRestartOperation{ID: id, Unit: operation.Unit, State: client.RestartSucceeded}, nil
	}, time.Millisecond, time.Second)
	if err != nil || result.State != client.RestartSucceeded || calls.Load() != 2 || queriedID != operation.ID {
		t.Fatalf("operation status was not reconciled safely: result=%+v calls=%d id=%q err=%v", result, calls.Load(), queriedID, err)
	}
	_, err = waitServiceRestart(context.Background(), operation, func(context.Context, string) (client.ServiceRestartOperation, error) {
		return client.ServiceRestartOperation{}, &client.ServiceError{Code: "restart_transport"}
	}, 10*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("status timeout was not clearly reported as unknown: %v", err)
	}
}

func TestServiceTokenPathConfigurationSurvivesEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	tokenPath := filepath.Join(t.TempDir(), "token")
	logTokenPath := filepath.Join(t.TempDir(), "log-token")
	restartTokenPath := filepath.Join(t.TempDir(), "restart-token")
	for _, args := range [][]string{
		{"servers", "add", "--name", "local", "--address", "http://127.0.0.1:8787", "--service-token-file", tokenPath, "--service-log-token-file", logTokenPath, "--service-restart-token-file", restartTokenPath},
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
	if cfg.Servers[0].ServiceTokenFile != tokenPath || cfg.Servers[0].ServiceLogTokenFile != logTokenPath || cfg.Servers[0].ServiceRestartTokenFile != restartTokenPath {
		t.Fatal("edit discarded credential reference")
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--address", "http://127.0.0.1:8788"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Servers[0].ServiceTokenFile != "" || cfg.Servers[0].ServiceLogTokenFile != "" || cfg.Servers[0].ServiceRestartTokenFile != "" {
		t.Fatal("address change retained a credential for another server")
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--service-token-file", tokenPath, "--service-log-token-file", logTokenPath, "--service-restart-token-file", restartTokenPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, []string{"--config", path, "servers", "edit", "--name", "local", "--service-token-file", "", "--service-log-token-file", "", "--service-restart-token-file", ""}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), cliServiceToken) || strings.Contains(string(body), cliServiceLogToken) {
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
	for _, args := range [][]string{{"services", "list", "local", "--limit", "0"}, {"services", "list", "local", "--limit", "51"}, {"services", "show", "local", "*.service"}, {"services", "logs", "local", "*.service"}, {"services", "logs", "local", "worker.service", "--limit", "101"}, {"services", "logs", "local", "worker.service", "--before", "old", "--after", "new"}} {
		if _, err := execute(t, append([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, args...)); err == nil || strings.Contains(err.Error(), "not found") {
			t.Fatalf("invalid input reached server lookup: %v", err)
		}
	}
}

func TestServiceLogsCLIUsesDistinctCredentialAndSanitizesOutput(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		out.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/capabilities" {
			if request.Header.Get("Authorization") != "" {
				t.Error("capability discovery carried a credential")
			}
			_, _ = out.Write([]byte(`{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":true}}`))
			return
		}
		if request.URL.Path != "/api/v1/services/web.service/logs" || request.Header.Get("Authorization") != "Bearer "+cliServiceLogToken {
			t.Errorf("wrong log endpoint or credential: %s %q", request.URL.Path, request.Header.Get("Authorization"))
		}
		if request.URL.Query().Get("limit") != "2" || request.URL.Query().Get("since") != "2026-10-18T12:00:00Z" {
			t.Errorf("CLI log query not preserved: %s", request.URL.RawQuery)
		}
		_, _ = out.Write([]byte(`{"api_version":"v1","data":{"unit":"web.service","entries":[{"timestamp":"2026-10-18T12:30:00Z","priority":3,"message":"\u001b[31mfailed\u001b[0m\nworker café ✓","cursor":"opaque"}],"latest_cursor":"opaque"}}`))
	}))
	defer server.Close()
	logPath := filepath.Join(t.TempDir(), "log-token")
	if err := os.WriteFile(logPath, []byte(cliServiceLogToken), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{Servers: []config.Server{{Name: "local", Address: server.URL, ServiceLogTokenFile: logPath}}}); err != nil {
		t.Fatal(err)
	}
	output, err := execute(t, []string{"--config", configPath, "services", "logs", "local", "web.service", "--limit", "2", "--since", "2026-10-18T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2026-10-18T12:30:00Z", "err", "failed", "worker café ✓"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in CLI log output: %q", want, output)
		}
	}
	if strings.Contains(output, "\x1b") || strings.Contains(output, cliServiceLogToken) || !strings.Contains(output, `--after "opaque"`) || requests.Load() != 2 {
		t.Fatalf("unsafe output or unexpected request count %d: %q", requests.Load(), output)
	}
}

func TestServiceLogStreamingReportsEmptyAndFollowsOpaqueCursorUntilCanceled(t *testing.T) {
	var empty strings.Builder
	if err := streamServiceLogs(context.Background(), func(context.Context, client.ServiceLogQuery) (client.ServiceLogPage, error) {
		return client.ServiceLogPage{Unit: "worker.service", Entries: []client.ServiceLogEntry{}}, nil
	}, "worker.service", client.ServiceLogQuery{Limit: 1}, false, &empty, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty.String(), "No log entries") {
		t.Fatalf("empty log history was ambiguous: %q", empty.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	var query client.ServiceLogQuery
	var output, notices strings.Builder
	err := streamServiceLogs(ctx, func(_ context.Context, current client.ServiceLogQuery) (client.ServiceLogPage, error) {
		calls++
		if calls == 1 {
			cancel()
			return client.ServiceLogPage{Unit: "worker.service", Entries: []client.ServiceLogEntry{cliServiceLogEntry()}, LatestCursor: "opaque-latest", HasMore: true}, nil
		}
		query = current
		return client.ServiceLogPage{Unit: "worker.service", Entries: []client.ServiceLogEntry{}}, nil
	}, "worker.service", client.ServiceLogQuery{Limit: 1}, true, &output, &notices)
	if !errors.Is(err, context.Canceled) || calls != 2 || query.After != "opaque-latest" {
		t.Fatalf("follow did not drain by cursor and stop on cancel: calls=%d query=%+v err=%v", calls, query, err)
	}
	if !strings.Contains(output.String(), "message") || strings.Contains(output.String(), "opaque-latest") || !strings.Contains(notices.String(), "Following") {
		t.Fatalf("unsafe or incomplete follow output: %q / %q", output.String(), notices.String())
	}
}

func cliServiceLogEntry() client.ServiceLogEntry {
	return client.ServiceLogEntry{Timestamp: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Message: "message", Cursor: "entry-cursor"}
}
