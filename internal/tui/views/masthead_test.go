package views

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
)

func TestSaturnMastheadAdaptsToScreen(t *testing.T) {
	for _, art := range []string{saturn} {
		for _, r := range art {
			if r > 127 {
				t.Fatal("masthead artwork must remain plain ASCII")
			}
		}
	}
	large := renderMasthead(DashboardData{Width: 76, Height: 40, Version: "dev"})
	if !strings.Contains(large, ".odMMMMMM'") || lipgloss.Height(large) != 15 || lipgloss.Width(large) > 76 {
		t.Fatalf("Saturn masthead missing or overflowing: %q", large)
	}
	if strings.Contains(large, "| (_) |") {
		t.Fatal("large ASCII Orbit wordmark should be removed")
	}
	for _, data := range []DashboardData{{Width: 76, Height: 24}, {Width: 76, Height: 32}, {Width: 20, Height: 40}} {
		if header := renderMasthead(data); lipgloss.Height(header) != 1 || lipgloss.Width(header) > data.Width {
			t.Fatalf("small screen should have a compact header: %q", header)
		}
	}
}

func TestHostDetailsDoNotExposeURLCredentials(t *testing.T) {
	status := &server.Status{Server: config.Server{
		Address: "http://alice:secret@localhost:8787/private?token=hidden",
		SSH:     &config.SSHConfig{Host: "prod", User: "root", Port: 2222},
	}, TunnelState: server.TunnelConnected}
	view := hostDetails(status, 38)
	for _, omit := range []string{"alice", "secret", "private", "hidden"} {
		if strings.Contains(view, omit) {
			t.Errorf("host details exposed %q: %q", omit, view)
		}
	}
	for _, want := range []string{"http://localhost:8787", "root@prod:2222", "SSH tunnel: connected"} {
		if !strings.Contains(view, want) {
			t.Errorf("host details missing %q: %q", want, view)
		}
	}
}

func TestNetworkCountersAreNotRatesAndRespectErrors(t *testing.T) {
	status := &server.Status{Metrics: &client.MetricsResponse{Data: client.MetricsData{
		Network: client.Network{BytesReceived: 1024, BytesSent: 2048, PacketsReceived: 10, PacketsSent: 20},
	}}}
	view := networkDetails(status, 76)
	for _, want := range []string{"cumulative counters", "1.0 KiB", "2.0 KiB", "10 packets", "20 packets"} {
		if !strings.Contains(view, want) {
			t.Errorf("network details missing %q: %q", want, view)
		}
	}
	status.Metrics.Data.Errors = []client.MetricError{{Metric: "network", Message: "collector failed"}}
	view = networkDetails(status, 76)
	if !strings.Contains(view, "unavailable") || strings.Contains(view, "1.0 KiB") {
		t.Fatalf("failed network collector should hide its counters: %q", view)
	}
}
