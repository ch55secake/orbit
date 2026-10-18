package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

func TestStatusCommandShowsImplementedStardustMetrics(t *testing.T) {
	response := statusFixture(t, "metrics-current.json")
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/metrics" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		out.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(out).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, config.Config{Servers: []config.Server{{Name: "local", Address: server.URL}}}); err != nil {
		t.Fatal(err)
	}
	output, err := execute(t, []string{"--config", path, "status"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status: online", "CPU: 12.5% (4 cores)", "Memory: 50.0%", "Swap: 30.0% (300 B / 1000 B)", "Disk: 50.0%", "Uptime: 1d 00h", "Network RX: 1000 B (20 packets)", "Network TX: 500 B (10 packets)", "Interface eth0: RX 1000 B (20 packets), TX 500 B (10 packets)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q:\n%s", want, output)
		}
	}
}

func TestStatusSwapCompatibilityAndFailures(t *testing.T) {
	for _, test := range []struct{ fixture, want string }{
		{"metrics-complete.json", ""},
		{"metrics-swap.json", "Swap: 25.0% (512.0 MiB / 2.0 GiB)"},
		{"metrics-no-swap.json", "Swap: not configured"},
		{"metrics-swap-unavailable.json", "Swap: unavailable"},
	} {
		output := statusSnapshot(t, statusFixture(t, test.fixture))
		if test.want == "" && strings.Contains(output, "Swap:") || test.want != "" && !strings.Contains(output, test.want) {
			t.Fatalf("unexpected optional swap output:\n%s", output)
		}
	}
	response := statusFixture(t, "metrics-current.json")
	response.Data.Swap.UsedBytes = response.Data.Swap.TotalBytes + 1
	if output := statusSnapshot(t, response); !strings.Contains(output, "Swap: unavailable") {
		t.Fatal("invalid swap sample displayed as healthy")
	}
	// Stardust emits a zero-valued swap object plus data.errors on failure.
	response.Data.Swap = &client.Swap{}
	response.Data.Errors = []client.MetricError{{Metric: "swap", Message: "swap collection failed"}}
	if output := statusSnapshot(t, response); !strings.Contains(output, "Swap: unavailable") || strings.Contains(output, "Swap: not configured") || !strings.Contains(output, "Network RX:") {
		t.Fatalf("failed swap confused with zero capacity or hid network:\n%s", output)
	}
}

func TestStatusPreservesAggregateAndAllInterfaceCounters(t *testing.T) {
	response := statusFixture(t, "metrics-interfaces.json")
	response.Data.Network.Interfaces = append(response.Data.Network.Interfaces, client.NetworkInterface{Name: "\x1b[31mbridge0\x1b[0m\n\r", BytesReceived: 1048576, BytesSent: 2097152, PacketsReceived: 9, PacketsSent: 8})
	response.Data.Network.Interfaces = append(response.Data.Network.Interfaces, client.NetworkInterface{Name: "\n"})
	output := statusSnapshot(t, response)
	for _, want := range []string{"Network RX: 1.0 MiB (1500 packets)", "Network TX: 2.0 MiB (1200 packets)", "Interface eth0", "Interface lo", "Interface bridge0: RX 1.0 MiB (9 packets), TX 2.0 MiB (8 packets)", "Interface (unnamed): RX 0 B (0 packets), TX 0 B (0 packets)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "\x1b") {
		t.Fatal("remote interface name emitted terminal sequences")
	}
	response.Data.Errors = []client.MetricError{{Metric: "network", Message: "counters unavailable"}}
	output = statusSnapshot(t, response)
	if !strings.Contains(output, "Network: unavailable") || strings.Contains(output, "Interface ") || strings.Contains(output, "Network RX:") || !strings.Contains(output, "CPU: 23.5%") {
		t.Fatalf("network failure rendered healthy counters or hid CPU:\n%s", output)
	}
}

func TestStatusValidZeroCountersAndAllWriterFailures(t *testing.T) {
	response := statusFixture(t, "metrics-current.json")
	response.Data.Network = client.Network{Interfaces: []client.NetworkInterface{{Name: "lo"}}}
	output := statusSnapshot(t, response)
	for _, want := range []string{"Network RX: 0 B (0 packets)", "Network TX: 0 B (0 packets)", "Interface lo: RX 0 B (0 packets), TX 0 B (0 packets)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("valid zero counters lost: %q", output)
		}
	}
	want := errors.New("write failure")
	count := &metricFailWriter{}
	if err := printMetrics(count, response.Data); err != nil {
		t.Fatal(err)
	}
	for failAt := 1; failAt <= count.calls; failAt++ {
		if err := printMetrics(&metricFailWriter{failAt: failAt, err: want}, response.Data); !errors.Is(err, want) {
			t.Fatalf("write %d: %v", failAt, err)
		}
	}
}

func TestCurrentFixtureMatchesDocumentedPayload(t *testing.T) {
	// Check the checked-in JSON itself, not just a re-encoded typed response:
	// these are implemented swap/network fields, not speculative extensions.
	body, err := os.ReadFile(filepath.Join("..", "client", "testdata", "stardust", "v1", "metrics-current.json"))
	if err != nil {
		t.Fatal(err)
	}
	var response client.MetricsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Swap == nil || response.Data.Swap.FreeBytes != 700 || len(response.Data.Network.Interfaces) != 1 || response.Data.Network.Interfaces[0].PacketsReceived != 20 {
		t.Fatal("implemented fields lost during decoding")
	}
}
