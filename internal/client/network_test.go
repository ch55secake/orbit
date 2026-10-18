package client

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientDecodesOptionalNetworkInterfaces(t *testing.T) {
	for _, fixture := range []string{"metrics-complete.json", "metrics-interfaces.json"} {
		t.Run(fixture, func(t *testing.T) {
			server := fixtureServer(t, "/api/v1/metrics", http.StatusOK, readFixture(t, fixture))
			defer server.Close()
			response, err := New(time.Second).Metrics(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			network := response.Data.Network
			if network.BytesReceived != 1048576 || network.BytesSent != 2097152 || network.PacketsReceived != 1500 || network.PacketsSent != 1200 {
				t.Fatalf("aggregate counters changed: %+v", network)
			}
			var want []NetworkInterface
			if fixture == "metrics-interfaces.json" {
				want = []NetworkInterface{
					{Name: "eth0", BytesReceived: 786432, BytesSent: 1572864, PacketsReceived: 1000, PacketsSent: 800},
					{Name: "lo", BytesReceived: 262144, BytesSent: 524288, PacketsReceived: 500, PacketsSent: 400},
				}
			}
			if !reflect.DeepEqual(network.Interfaces, want) {
				t.Fatalf("interfaces = %+v, want %+v", network.Interfaces, want)
			}
		})
	}
}

func TestEmptyAndNullNetworkInterfaces(t *testing.T) {
	for _, field := range []string{``, `,"interfaces":null`, `,"interfaces":[]`} {
		var response MetricsResponse
		if err := json.Unmarshal([]byte(`{"api_version":"v1","data":{"network":{"bytes_received":123`+field+`}}}`), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data.Network.Interfaces) != 0 || response.Data.Network.BytesReceived != 123 {
			t.Fatal("empty optional list changed aggregate counters")
		}
	}
}

func TestClientPreservesPartialNetworkResponse(t *testing.T) {
	body := strings.Replace(readFixture(t, "metrics-interfaces.json"), `"hostname":`, `"errors":[{"metric":"network","message":"collection incomplete"}],"hostname":`, 1)
	server := fixtureServer(t, "/api/v1/metrics", http.StatusOK, body)
	defer server.Close()
	response, err := New(time.Second).Metrics(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Network.Interfaces) != 2 || len(response.Data.Errors) != 1 || response.Data.Errors[0].Metric != "network" || response.Data.CPU.UsagePercent != 23.5 {
		t.Fatalf("partial response lost samples/error: %+v", response.Data)
	}
}
