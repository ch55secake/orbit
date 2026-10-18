package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const serviceTestToken = "0123456789abcdef0123456789abcdef"
const serviceLogTestToken = "abcdef0123456789abcdef0123456789"
const serviceRestartTestToken = "fedcba9876543210fedcba9876543210"
const serviceCapabilitiesJSON = `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer"}}`

func serviceTestClient(t *testing.T, handler http.HandlerFunc) *ServicesClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(serviceTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func writeServiceJSON(t *testing.T, out http.ResponseWriter, body string) {
	t.Helper()
	out.Header().Set("Content-Type", "application/json")
	if _, err := out.Write([]byte(body)); err != nil {
		t.Error(err)
	}
}

func TestServicesImplementedProducerFixtureAndDetail(t *testing.T) {
	body, err := os.ReadFile("testdata/stardust/v1/services-states.json")
	if err != nil {
		t.Fatal(err)
	}
	var query string
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.Method != "GET" {
			t.Error("service mutation attempted")
		}
		if request.URL.Path == "/api/v1/capabilities" {
			if request.Header.Get("Authorization") != "" {
				t.Error("token sent to capability discovery")
			}
			writeServiceJSON(t, out, serviceCapabilitiesJSON)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+serviceTestToken {
			t.Error("missing service authorization")
		}
		query = request.URL.RawQuery
		if request.URL.Path == "/api/v1/services" {
			writeServiceJSON(t, out, string(body))
			return
		}
		if request.URL.Path != "/api/v1/services/worker@blue.service" {
			t.Errorf("unexpected detail path: %s", request.URL.Path)
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"name":"worker@blue.service","id":"worker@blue.service","main_pid":456,"main_process":{"name":"worker","effective_uid":1001}}}`)
	})
	page, err := api.List(context.Background(), ServiceQuery{Filter: "worker & café", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Units) != 4 || page.Units[0].MainProcess.Name != "worker" || page.Units[1].ActiveState != "failed" || page.Units[2].MainPID != 0 || page.Units[3].LoadState != "not-found" {
		t.Fatalf("lost producer fields: %+v", page)
	}
	if !strings.Contains(query, "filter=worker+%26+caf%C3%A9") {
		t.Fatalf("unsafe query encoding: %s", query)
	}
	detail, err := api.Detail(context.Background(), "worker@blue.service")
	if err != nil || detail.MainPID != 456 || detail.MainProcess.EffectiveUID != 1001 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
}

func TestServiceInventoryCarriesRestartCapabilityAndPerUnitAuthorization(t *testing.T) {
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","restart":true,"restart_authentication":"bearer"}}`)
			return
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"units":[{"name":"readonly.service","id":"readonly.service"},{"name":"worker.service","id":"worker.service","restart_allowed":true}]}}`)
	})
	page, err := api.List(context.Background(), ServiceQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Capabilities.Restart || page.Capabilities.RestartAuthentication != "bearer" || len(page.Units) != 2 || page.Units[0].RestartAllowed || !page.Units[1].RestartAllowed {
		t.Fatalf("restart authorization data was not preserved: %+v", page)
	}
}

func TestServicesCapabilityAndErrorMessages(t *testing.T) {
	for _, test := range []struct {
		name, capability, code, want string
		status                       int
	}{
		{"legacy", "legacy", "", "upgrade Stardust", 404},
		{"unsupported", `{"api_version":"v1","services":{"enabled":true,"supported":false}}`, "", "systemd Linux", 200},
		{"disabled", `{"api_version":"v1","services":{"supported":true}}`, "", "disabled", 200},
		{"scheme", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"other"}}`, "", "authentication", 200},
		{"unauthorized", serviceCapabilitiesJSON, "unauthorized", "token was rejected", 401},
		{"forbidden", serviceCapabilitiesJSON, "unit_forbidden", "allowlist", 403},
		{"missing", serviceCapabilitiesJSON, "unit_not_found", "no longer exists", 404},
		{"permissions", serviceCapabilitiesJSON, "services_permission_denied", "permission", 503},
		{"timeout", serviceCapabilitiesJSON, "services_timeout", "timed out", 504},
		{"backend", serviceCapabilitiesJSON, "services_unavailable", "unavailable", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/v1/capabilities" {
					if test.capability == "legacy" {
						out.WriteHeader(test.status)
					}
					writeServiceJSON(t, out, test.capability)
					return
				}
				out.WriteHeader(test.status)
				body, err := json.Marshal(map[string]any{"api_version": "v1", "error": map[string]string{"code": test.code, "message": serviceTestToken + "\x1b[31m"}})
				if err != nil {
					t.Error(err)
					return
				}
				writeServiceJSON(t, out, string(body))
			})
			_, err := api.List(context.Background(), ServiceQuery{})
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), serviceTestToken) || strings.Contains(err.Error(), "\x1b") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}

func TestServicesNeverForwardCredentialsOnRedirect(t *testing.T) {
	var calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer other.Close()
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, serviceCapabilitiesJSON)
			return
		}
		http.Redirect(out, request, other.URL, http.StatusTemporaryRedirect)
	})
	if _, err := api.List(context.Background(), ServiceQuery{}); err == nil {
		t.Fatal("redirect followed")
	}
	if calls.Load() != 0 {
		t.Fatal("credential redirect reached a different server")
	}
	for _, address := range []string{"http://remote.test:8787", "http://localhost:8787", "http://user:secret@127.0.0.1", "https://remote.test?secret=x", "file:///etc/passwd"} {
		if _, err := NewServices(address, "", 0); err == nil {
			t.Errorf("unsafe origin accepted: %s", address)
		}
	}
}

func TestServicesValidatePaginationIdentityAndBodyBounds(t *testing.T) {
	for _, body := range []string{
		`{"api_version":"v1","data":{"units":[],"next_cursor":"a.service"}}`,
		`{"api_version":"v1","data":{"units":[{"name":"a.service"}],"next_cursor":"b.service"}}`,
		`{"api_version":"v1","data":{"units":[{"name":"b.service"},{"name":"a.service"}]}}`,
		`{"api_version":"v1","data":{"units":[{"name":"bad;id.service"}]}}`,
		`{"api_version":"` + serviceTestToken + `","data":{}}`,
		strings.Repeat("x", 4*1024*1024+1),
	} {
		api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/api/v1/capabilities" {
				writeServiceJSON(t, out, serviceCapabilitiesJSON)
			} else {
				writeServiceJSON(t, out, body)
			}
		})
		if _, err := api.List(context.Background(), ServiceQuery{}); err == nil || strings.Contains(err.Error(), serviceTestToken) {
			t.Fatalf("invalid response accepted or secret leaked: %v", err)
		}
	}
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, serviceCapabilitiesJSON)
			return
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"name":"wrong.service"}}`)
	})
	if _, err := api.Detail(context.Background(), "a.service"); err == nil {
		t.Fatal("wrong detail identity accepted")
	}
	for _, name := range []string{"", "*.service", "--help.service", "../../etc.service"} {
		if _, err := api.Detail(context.Background(), name); err == nil {
			t.Fatal("invalid unit accepted")
		}
	}
	for _, query := range []ServiceQuery{{Limit: 51}, {Limit: -1}, {Filter: strings.Repeat("x", 256)}, {Cursor: "../bad"}} {
		if _, err := api.List(context.Background(), query); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
}

func TestServicesCancellationRedactionAndSafeText(t *testing.T) {
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, serviceCapabilitiesJSON)
			return
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"name":"a.service","description":"`+serviceTestToken+`"}}`)
	})
	unit, err := api.Detail(context.Background(), "a.service")
	if err != nil || strings.Contains(unit.Description, serviceTestToken) || unit.Description != "[redacted]" {
		t.Fatalf("reflected credential: %+v %v", unit, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.List(ctx, ServiceQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if text := ServiceText("\x1b[31mhello\x1b[0m\nworld\r\t\u202e✓"); strings.ContainsAny(text, "\x1b\n\r\t\u202e") || !strings.Contains(text, "hello") || !strings.Contains(text, "✓") {
		t.Fatalf("unsafe terminal text: %q", text)
	}
}

func TestServicesCursorRoundTripAndTokenRotation(t *testing.T) {
	var expected atomic.Value
	expected.Store(serviceTestToken)
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, serviceCapabilitiesJSON)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+expected.Load().(string) {
			t.Error("stale token used")
		}
		if request.URL.Query().Get("cursor") == "a.service" {
			writeServiceJSON(t, out, `{"api_version":"v1","data":{"units":[{"name":"b.service"}]}}`)
			return
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"units":[{"name":"a.service"}],"next_cursor":"a.service"}}`)
	})
	page, err := api.List(context.Background(), ServiceQuery{Limit: 1})
	if err != nil || page.NextCursor != "a.service" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	newToken := strings.Repeat("b", 32)
	expected.Store(newToken)
	if err := os.WriteFile(api.tokenFile, []byte(newToken), 0600); err != nil {
		t.Fatal(err)
	}
	page, err = api.List(context.Background(), ServiceQuery{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(page.Units) != 1 || page.Units[0].Name != "b.service" || page.NextCursor != "" {
		t.Fatalf("next page: %+v %v", page, err)
	}
}

func TestServiceLogsUseSeparateCredentialAndValidateStructuredPage(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/api/v1/capabilities" {
			if request.Header.Get("Authorization") != "" {
				t.Error("credential sent to capability discovery")
			}
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":true}}`)
			return
		}
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+serviceLogTestToken {
			t.Error("service log request did not use its separate bearer token")
		}
		if request.URL.Path != "/api/v1/services/web-alias.service/logs" || request.URL.Query().Get("limit") != "1" || request.URL.Query().Get("after") != "opaque-after" || request.URL.Query().Get("since") != "2026-10-18T11:00:00Z" {
			t.Errorf("log query was not encoded safely: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"unit":"web.service","entries":[{"timestamp":"2026-10-18T12:00:00Z","priority":6,"message":"first\nsecond café ✓","cursor":"opaque-page"}],"next_cursor":"opaque-page","latest_cursor":"opaque-page","has_more":true}}`)
	}))
	defer server.Close()
	logTokenPath := filepath.Join(t.TempDir(), "log-token")
	if err := os.WriteFile(logTokenPath, []byte(serviceLogTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	page, err := api.Logs(context.Background(), logTokenPath, "web-alias.service", ServiceLogQuery{Limit: 1, After: "opaque-after", Since: time.Date(2026, 10, 18, 12, 0, 0, 0, time.FixedZone("offset", 3600))})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || page.Unit != "web.service" || len(page.Entries) != 1 || page.Entries[0].Message != "first\nsecond café ✓" || page.LatestCursor != "opaque-page" || !page.HasMore {
		t.Fatalf("invalid service log page: %+v (requests=%d)", page, requests.Load())
	}
}

func TestServiceRestartUsesSeparateCredentialAndSameIdentityOnRetry(t *testing.T) {
	var postCalls atomic.Int32
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			if request.Header.Get("Authorization") != "" {
				t.Error("capability discovery carried a credential")
			}
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","restart":true,"restart_authentication":"bearer"}}`)
			return
		}
		postCalls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/services/web-alias.service/restart" {
			t.Errorf("unexpected restart request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer "+serviceRestartTestToken {
			t.Error("restart did not use the separate restart credential")
		}
		if request.Header.Get("Origin") != "" || request.ContentLength != 0 {
			t.Error("restart request included an origin or body")
		}
		key := request.Header.Get("Idempotency-Key")
		if receivedKey == "" {
			receivedKey = key
		} else if key != receivedKey {
			t.Errorf("retry changed idempotency key: %q vs %q", key, receivedKey)
		}
		if postCalls.Load() == 1 {
			out.WriteHeader(http.StatusServiceUnavailable)
			writeServiceJSON(t, out, `{"api_version":"v1","error":{"code":"temporary"}}`)
			return
		}
		out.WriteHeader(http.StatusAccepted)
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"operation_id":"0123456789abcdef0123456789abcdef","unit":"web-alias.service","state":"running","requested_at":"2026-10-18T12:00:00Z"}}`)
	}))
	defer server.Close()
	readPath := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(readPath, []byte(serviceTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	restartPath := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(restartPath, []byte(serviceRestartTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, readPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewServiceRestartKey()
	if err != nil || len(key) != 64 {
		t.Fatalf("restart key generation: %q %v", key, err)
	}
	operation, err := api.Restart(context.Background(), restartPath, "web-alias.service", key)
	if err != nil {
		t.Fatal(err)
	}
	if postCalls.Load() != 2 || receivedKey != key || operation.State != RestartRunning || operation.ID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("restart was not safely reconciled: calls=%d key=%q operation=%+v", postCalls.Load(), receivedKey, operation)
	}
}

func TestServiceRestartRetriesDisconnectedSubmissionWithSameIdentity(t *testing.T) {
	var posts atomic.Int32
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"supported":true,"restart":true,"restart_authentication":"bearer"}}`)
			return
		}
		posts.Add(1)
		if key == "" {
			key = request.Header.Get("Idempotency-Key")
		} else if key != request.Header.Get("Idempotency-Key") {
			t.Errorf("disconnect retry changed identity: %q != %q", key, request.Header.Get("Idempotency-Key"))
		}
		if posts.Load() == 1 {
			connection, _, err := out.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		out.WriteHeader(http.StatusAccepted)
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"operation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","unit":"worker.service","state":"running","requested_at":"2026-10-18T12:00:00Z"}}`)
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(tokenPath, []byte(serviceRestartTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := api.Restart(context.Background(), tokenPath, "worker.service", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 || operation.ID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || key != strings.Repeat("b", 64) {
		t.Fatalf("disconnect was not reconciled with the original key: posts=%d key=%q operation=%+v", posts.Load(), key, operation)
	}
}

func TestServiceRestartStatusUsesSeparateCredentialAndValidatesIdentity(t *testing.T) {
	var statusCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"supported":true,"restart":true,"restart_authentication":"bearer"}}`)
			return
		}
		statusCalls.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/service-operations/0123456789abcdef0123456789abcdef" || request.Header.Get("Authorization") != "Bearer "+serviceRestartTestToken {
			t.Errorf("unexpected operation status request: %s %s %q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"operation_id":"0123456789abcdef0123456789abcdef","unit":"worker.service","state":"succeeded","requested_at":"2026-10-18T12:00:00Z","completed_at":"2026-10-18T12:00:02Z","job_result":"succeeded","active_state":"active","sub_state":"running"}}`)
	}))
	defer server.Close()
	readPath := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(readPath, []byte(serviceTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	restartPath := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(restartPath, []byte(serviceRestartTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, readPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := api.RestartStatus(context.Background(), restartPath, "0123456789abcdef0123456789abcdef")
	if err != nil || operation.State != RestartSucceeded || statusCalls.Load() != 1 {
		t.Fatalf("restart status: %+v %v (calls %d)", operation, err, statusCalls.Load())
	}
	if _, err := api.RestartStatus(context.Background(), restartPath, "not-an-operation"); err == nil {
		t.Fatal("invalid operation identifier accepted")
	}
}

func TestServiceRestartPreflightAndErrorRedaction(t *testing.T) {
	for _, test := range []struct {
		name, capabilities, errorCode, want string
		posts                               int32
	}{
		{"disabled", `{"api_version":"v1","services":{"supported":true,"restart":false}}`, "", "unavailable", 0},
		{"unauthorized", `{"api_version":"v1","services":{"supported":true,"restart":true,"restart_authentication":"bearer"}}`, "unauthorized", "token was rejected", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/v1/capabilities" {
					writeServiceJSON(t, out, test.capabilities)
					return
				}
				posts.Add(1)
				out.WriteHeader(http.StatusUnauthorized)
				writeServiceJSON(t, out, `{"api_version":"v1","error":{"code":"`+test.errorCode+`","message":"`+serviceRestartTestToken+`"}}`)
			}))
			defer server.Close()
			tokenPath := filepath.Join(t.TempDir(), "restart-token")
			if err := os.WriteFile(tokenPath, []byte(serviceRestartTestToken), 0600); err != nil {
				t.Fatal(err)
			}
			api, err := NewServices(server.URL, "", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Restart(context.Background(), tokenPath, "worker.service", strings.Repeat("a", 64))
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), serviceRestartTestToken) {
				t.Fatalf("unexpected or unsafe restart error: %v", err)
			}
			if posts.Load() != test.posts {
				t.Fatalf("got %d restart requests, want %d", posts.Load(), test.posts)
			}
		})
	}
	api := serviceTestClient(t, func(out http.ResponseWriter, request *http.Request) { t.Error("invalid request reached the API") })
	if _, err := api.Restart(context.Background(), "", "*.service", "short"); err == nil {
		t.Fatal("invalid restart request accepted")
	}
}

func TestServiceRestartNeverExposesRestartCredentialInOperationData(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			writeServiceJSON(t, out, `{"api_version":"v1","services":{"supported":true,"restart":true,"restart_authentication":"bearer"}}`)
			return
		}
		posts.Add(1)
		out.WriteHeader(http.StatusAccepted)
		writeServiceJSON(t, out, `{"api_version":"v1","data":{"operation_id":"`+serviceRestartTestToken+`","unit":"worker.service","state":"running","requested_at":"2026-10-18T12:00:00Z"}}`)
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(tokenPath, []byte(serviceRestartTestToken), 0600); err != nil {
		t.Fatal(err)
	}
	api, err := NewServices(server.URL, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.Restart(context.Background(), tokenPath, "worker.service", strings.Repeat("a", 64))
	if err == nil || strings.Contains(err.Error(), serviceRestartTestToken) || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("credential reflected by operation response: %v", err)
	}
	if posts.Load() != 3 {
		t.Fatalf("ambiguous response was not bounded with same-identity retries: %d", posts.Load())
	}
}

func TestServiceLogsCapabilityAuthorizationAndMalformedResponses(t *testing.T) {
	for _, test := range []struct {
		name, capability, body, want string
		status                       int
		logCalls                     int32
	}{
		{"disabled", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":false}}`, `{"api_version":"v1","error":{"code":"logs_disabled"}}`, "disabled", http.StatusServiceUnavailable, 1},
		{"unsupported journal", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":false}}`, `{"api_version":"v1","error":{"code":"logs_unsupported"}}`, "unsupported", http.StatusNotImplemented, 1},
		{"unsupported host", `{"api_version":"v1","services":{"enabled":true,"supported":false,"authentication":"bearer","logs":false}}`, "", "unsupported", 0, 0},
		{"inventory disabled", `{"api_version":"v1","services":{"enabled":false,"supported":true,"authentication":"bearer","logs":false}}`, "", "disabled", 0, 0},
		{"permission", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":true}}`, `{"api_version":"v1","error":{"code":"logs_permission_denied"}}`, "permission to read", http.StatusServiceUnavailable, 1},
		{"expired", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":true}}`, `{"api_version":"v1","error":{"code":"log_cursor_expired"}}`, "cursor expired", http.StatusGone, 1},
		{"invalid page", `{"api_version":"v1","services":{"enabled":true,"supported":true,"authentication":"bearer","logs":true}}`, `{"api_version":"v1","data":{"unit":"web.service","entries":[{"timestamp":"2026-10-18T12:00:00Z","message":"bad","cursor":""}]}}`, "invalid service log entry", http.StatusOK, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/v1/capabilities" {
					writeServiceJSON(t, out, test.capability)
					return
				}
				logCalls.Add(1)
				if test.status != http.StatusOK {
					out.WriteHeader(test.status)
				}
				writeServiceJSON(t, out, test.body)
			}))
			defer server.Close()
			logPath := filepath.Join(t.TempDir(), "log-token")
			if err := os.WriteFile(logPath, []byte(serviceLogTestToken), 0600); err != nil {
				t.Fatal(err)
			}
			api, err := NewServices(server.URL, "", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Logs(context.Background(), logPath, "web.service", ServiceLogQuery{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected logs result: %v", err)
			}
			if logCalls.Load() != test.logCalls {
				t.Fatalf("unexpected log endpoint requests: %d", logCalls.Load())
			}
		})
	}
}

func TestServiceLogsMapOldAgentCapability404ToLogUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/capabilities" {
			out.WriteHeader(http.StatusNotFound)
			writeServiceJSON(t, out, `{"api_version":"v1","error":{"code":"not_found"}}`)
			return
		}
		t.Error("unsupported old agent received a log request")
	}))
	defer server.Close()
	api, err := NewServices(server.URL, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.Logs(context.Background(), "", "worker.service", ServiceLogQuery{})
	if err == nil || !strings.Contains(err.Error(), "Service logs are unsupported") {
		t.Fatalf("old-agent capability was not identified as log-unsupported: %v", err)
	}
}

func TestServiceLogTextPreservesMultilineUnicodeAndNeutralizesControls(t *testing.T) {
	got := ServiceLogText("\x1b[31mfirst\x1b[0m\nsecond café 👩\u200d💻\x00\u202eend")
	if got != "first\nsecond café 👩\u200d💻  end" {
		t.Fatalf("log text sanitization changed readable content unexpectedly: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\x00") {
		t.Fatalf("terminal controls leaked: %q", got)
	}
}
