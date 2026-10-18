package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

func statusSnapshot(t *testing.T, response client.MetricsResponse) string {
	t.Helper()
	var output bytes.Buffer
	api := &statusMetricsClient{response: response}
	if err := printStatus(context.Background(), &output, config.Config{Servers: []config.Server{{Name: "local", Address: "http://127.0.0.1:8787"}}}, api, nil, nil); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func statusFixture(t *testing.T, name string) client.MetricsResponse {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "client", "testdata", "stardust", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var response client.MetricsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestStatusRespectsPartialCollectorErrors(t *testing.T) {
	for _, test := range []struct{ fixture, unavailable, healthy, reason string }{
		{"metrics-partial.json", "Disk: unavailable", "CPU: 23.5%", "permission denied"},
		{"metrics-host-unavailable.json", "Uptime: unavailable", "Disk: 25.0%", "host metadata unavailable"},
	} {
		output := statusSnapshot(t, statusFixture(t, test.fixture))
		for _, want := range []string{"status: online", test.unavailable, test.healthy, test.reason, "unavailable: 1 metric(s)"} {
			if !strings.Contains(output, want) {
				t.Fatalf("missing %q:\n%s", want, output)
			}
		}
	}
}

func TestStatusDoesNotFabricateZeroOrInvalidReadings(t *testing.T) {
	response := statusFixture(t, "metrics-complete.json")
	response.Data.CPU.UsagePercent = 0
	response.Data.Uptime = 0
	output := statusSnapshot(t, response)
	if !strings.Contains(output, "CPU: 0.0%") || !strings.Contains(output, "Uptime: 00h 00m") {
		t.Fatal("legitimate zero usage/uptime lost")
	}
	response.Data.Errors = []client.MetricError{{Metric: "cpu", Message: "baseline unavailable"}, {Metric: "host", Message: "host unavailable"}}
	output = statusSnapshot(t, response)
	if !strings.Contains(output, "CPU: unavailable") || !strings.Contains(output, "Uptime: unavailable") || strings.Contains(output, "CPU: 0.0%") {
		t.Fatalf("failed sample rendered as zero:\n%s", output)
	}
	response.Data.Errors = nil
	response.Data.CPU.UsagePercent = math.NaN()
	response.Data.Memory.TotalBytes = 0
	response.Data.Disk.UsedPercent = 200
	output = statusSnapshot(t, response)
	for _, label := range []string{"CPU", "Memory", "Disk"} {
		if !strings.Contains(output, label+": unavailable") {
			t.Fatalf("invalid %s rendered as healthy", label)
		}
	}
}

func TestStatusCollectorReasonsArePlainSingleLineText(t *testing.T) {
	response := statusFixture(t, "metrics-complete.json")
	response.Data.Errors = []client.MetricError{{Metric: "cpu", Message: "\x1b[31mbaseline\x1b[0m\nretry later"}}
	output := statusSnapshot(t, response)
	if strings.Contains(output, "\x1b") || !strings.Contains(output, "cpu: baseline retry later") {
		t.Fatalf("collector message changed terminal output: %q", output)
	}
}

type metricFailWriter struct {
	calls, failAt int
	err           error
}

func (writer *metricFailWriter) Write(data []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failAt {
		return 0, writer.err
	}
	return len(data), nil
}

func TestStatusPropagatesMetricWriterErrors(t *testing.T) {
	data := client.MetricsData{Errors: []client.MetricError{{Metric: "cpu", Message: "unavailable"}}}
	want := errors.New("write failed")
	for failAt := 1; failAt <= 6; failAt++ {
		if err := printMetrics(&metricFailWriter{failAt: failAt, err: want}, data); !errors.Is(err, want) {
			t.Fatalf("write %d: got %v", failAt, err)
		}
	}
}
