package components

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestInterfaceSummary(t *testing.T) {
	data := client.MetricsData{Network: client.Network{Interfaces: []client.NetworkInterface{
		{Name: "eth0", BytesReceived: 1048576, BytesSent: 2097152},
		{Name: "lo"},
	}}}
	for _, width := range []int{20, 40, 80} {
		view := InterfaceSummary(data, width)
		plain := ansi.Strip(view)
		for _, want := range []string{"NETWORK", "eth0", "lo", "RX", "TX", "1.0 MiB", "2.0 MiB", "0 B"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q: %q", want, plain)
			}
		}
		if lipgloss.Width(view) > width {
			t.Fatal("interfaces exceed terminal width")
		}
	}
	data.Errors = []client.MetricError{{Metric: "NETWORK", Message: "collection failed"}}
	if view := ansi.Strip(InterfaceSummary(data, 80)); !strings.Contains(view, "unavailable") || strings.Contains(view, "eth0") {
		t.Fatalf("failed sample looks healthy: %q", view)
	}
	if InterfaceSummary(client.MetricsData{}, 80) != "" {
		t.Fatal("legacy metrics gained interface detail")
	}
}

func TestManyAndUntrustedInterfaceNamesStayBounded(t *testing.T) {
	var data client.MetricsData
	for index := range 100 {
		data.Network.Interfaces = append(data.Network.Interfaces, client.NetworkInterface{Name: fmt.Sprintf("veth-%03d-", index) + strings.Repeat("界", 60)})
	}
	data.Network.Interfaces[0].Name = "\x1b[31meth0\x1b[0m\n\r\t"
	for _, width := range []int{20, 40, 80} {
		view := InterfaceSummary(data, width)
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 12 {
			t.Fatalf("interface list not bounded at width %d", width)
		}
		if !strings.Contains(ansi.Strip(view), "+98 more interfaces") || strings.Contains(view, "\x1b[31m") {
			t.Fatal("interface count/sanitization lost")
		}
	}
}
