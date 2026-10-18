package views

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/charmbracelet/x/ansi"
)

func TestMetricErrorsCollapsePerMountFailures(t *testing.T) {
	var failures []client.MetricError
	for index := 0; index < 24; index++ {
		failures = append(failures, client.MetricError{Metric: "filesystems", Message: fmt.Sprintf("mount \"/var/lib/docker/rootfs/overlayfs/%064x\": permission denied", index)})
	}
	failures = append(failures, client.MetricError{Metric: "cpu", Message: "sample unavailable"})
	warnings := metricErrorWarnings(failures, 72)
	if len(warnings) != 1 {
		t.Fatalf("warning count=%d, want non-filesystem metric only: %q", len(warnings), warnings)
	}
	plain := ansi.Strip(strings.Join(warnings, "\n"))
	for _, want := range []string{"CPU: sample unavailable"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if strings.Contains(plain, "/var/lib/docker") || strings.Contains(plain, "overlayfs") || strings.Contains(plain, "FILESYSTEMS") {
		t.Fatalf("filesystem details duplicated in warnings: %q", plain)
	}
	if lipgloss.Height(strings.Join(warnings, "\n")) != 1 {
		t.Fatalf("warnings wrapped into extra rows: %q", plain)
	}
}

func TestMetricErrorsBoundLongUnsafeAndExcessWarnings(t *testing.T) {
	failures := []client.MetricError{
		{Metric: "\x1b[31mfilesystem\x1b[0m", Message: "\npermission\x1b[2J denied"},
		{Metric: "cpu", Message: strings.Repeat("long error ", 50)},
		{Metric: "memory", Message: "unavailable"},
		{Metric: "swap", Message: "unavailable"},
		{Metric: "network", Message: "unavailable"},
	}
	for _, width := range []int{1, 8, 20, 40, 80} {
		warnings := metricErrorWarnings(failures, width)
		if len(warnings) != 4 {
			t.Fatalf("width %d warning count=%d", width, len(warnings))
		}
		for _, warning := range warnings {
			if lipgloss.Width(warning) > width || strings.Contains(warning, "\x1b[2J") || strings.Contains(warning, "\n") {
				t.Fatalf("unsafe/unbounded width %d: %q", width, warning)
			}
		}
		if width >= 40 && !strings.Contains(ansi.Strip(warnings[3]), "2 more metric warnings") {
			t.Fatal("excess metric errors not summarized")
		}
	}
}

func TestDashboardRemainsCompactForManyFilesystemErrors(t *testing.T) {
	errors := make([]client.MetricError, 0, 30)
	for index := 0; index < 30; index++ {
		errors = append(errors, client.MetricError{Metric: "filesystems", Message: fmt.Sprintf("mount \"/var/lib/docker/rootfs/overlayfs/%064x\": permission denied", index)})
	}
	status := server.Status{Server: config.Server{Name: "mele"}, Metrics: &client.MetricsResponse{Data: client.MetricsData{
		Hostname: "mele", OS: client.OSInfo{Name: "linux", PlatformVersion: "24.04", KernelArch: "x86_64"},
		CPU: client.CPU{UsagePercent: 84.9, LogicalCores: 4}, Memory: client.Memory{UsedBytes: 3 << 30, TotalBytes: 8 << 30, UsedPercent: 42},
		Disk: client.Disk{UsedBytes: 59 << 30, TotalBytes: 110 << 30, UsedPercent: 56}, Filesystems: []client.Filesystem{{MountPoint: "/", UsedBytes: 59 << 30, TotalBytes: 110 << 30, UsedPercent: 56}}, Errors: errors,
	}}}
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}} {
		view := Render(DashboardData{Width: size[0], Height: size[1], Table: "FLEET", Selected: &status, Statuses: []server.Status{status}, SelectedIndex: 0})
		plain := ansi.Strip(view)
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("dashboard overflow at %dx%d (%dx%d):\n%s", size[0], size[1], lipgloss.Width(view), lipgloss.Height(view), plain)
		}
		if strings.Contains(plain, "mount(s) unavailable") || strings.Contains(plain, "/var/lib/docker") {
			t.Fatalf("per-mount errors still dominate %dx%d:\n%s", size[0], size[1], plain)
		}
		if !strings.Contains(plain, "FILESYSTEMS · partial · 30 mounts unavailable") || !strings.Contains(plain, "CPU") {
			t.Fatalf("other dashboard content lost:\n%s", plain)
		}
	}
}
