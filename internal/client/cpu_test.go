package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestClientDecodesOptionalCPUDetails(t *testing.T) {
	for _, fixture := range []string{"metrics-complete.json", "metrics-cpu-detail.json", "metrics-cpu-detail-partial.json"} {
		t.Run(fixture, func(t *testing.T) {
			server := fixtureServer(t, "/api/v1/metrics", http.StatusOK, readFixture(t, fixture))
			defer server.Close()
			response, err := New(time.Second).Metrics(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			cpu := response.Data.CPU
			if cpu.UsagePercent != 23.5 || response.Data.Memory.UsedPercent != 37.5 {
				t.Fatal("extension changed aggregate CPU/memory")
			}
			if fixture == "metrics-complete.json" {
				if cpu.LoadAverage != nil || len(cpu.PerCoreUsagePercent) != 0 || cpu.LogicalCores != 8 {
					t.Fatal("legacy CPU fields changed")
				}
				return
			}
			if cpu.LogicalCores != 4 || len(cpu.PerCoreUsagePercent) != 4 || cpu.PerCoreUsagePercent[1] == nil || *cpu.PerCoreUsagePercent[1] != 42.5 || cpu.LoadAverage == nil {
				t.Fatalf("optional CPU fields lost: %+v", cpu)
			}
			load := cpu.LoadAverage
			if fixture == "metrics-cpu-detail.json" {
				if cpu.PerCoreUsagePercent[0] == nil || *cpu.PerCoreUsagePercent[0] != 0 || load.OneMinute == nil || *load.OneMinute != 0.5 || load.FiveMinutes == nil || *load.FiveMinutes != 1.25 || load.FifteenMinutes == nil || *load.FifteenMinutes != 2 {
					t.Fatal("valid zero core/load samples lost")
				}
			} else if cpu.PerCoreUsagePercent[0] != nil || load.OneMinute != nil || load.FifteenMinutes != nil || load.FiveMinutes == nil || *load.FiveMinutes != 0 {
				t.Fatal("absent samples became zero or valid zero became absent")
			}
		})
	}
}

func TestAbsentNullAndEmptyCPUDetails(t *testing.T) {
	for _, fields := range []string{"", `,"load_average":null,"per_core_usage_percent":null`, `,"per_core_usage_percent":[]`} {
		var response MetricsResponse
		if err := json.Unmarshal([]byte(`{"api_version":"v1","data":{"cpu":{"usage_percent":42`+fields+`}}}`), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.CPU.LoadAverage != nil || len(response.Data.CPU.PerCoreUsagePercent) != 0 || response.Data.CPU.UsagePercent != 42 {
			t.Fatal("empty optional CPU data changed aggregate usage")
		}
	}
}
