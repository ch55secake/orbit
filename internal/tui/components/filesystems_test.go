package components

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestFilesystemSummaryPrioritizesFullMounts(t *testing.T) {
	data := client.MetricsData{Filesystems: []client.Filesystem{
		{MountPoint: "/", TotalBytes: 1024, UsedBytes: 512, AvailableBytes: 500, UsedPercent: 50},
		{MountPoint: "/data", TotalBytes: 1024, UsedBytes: 1000, AvailableBytes: 20, UsedPercent: 97.7},
		{MountPoint: "/backup", TotalBytes: 1024, UsedBytes: 768, AvailableBytes: 250, UsedPercent: 75},
	}}
	wantOrder := slices.Clone(data.Filesystems)
	for _, width := range []int{24, 40, 80} {
		view := FilesystemSummary(data, width)
		plain := ansi.Strip(view)
		for _, want := range []string{"FILESYSTEMS", "/data", "97.7%", "/backup", "75.0%", "+1 more mount"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q: %q", want, plain)
			}
		}
		if strings.Index(plain, "/data") > strings.Index(plain, "/backup") {
			t.Fatal("fullest mount not first")
		}
		if width == 80 && !strings.Contains(plain, "1000 B / 1.0 KiB") {
			t.Fatal("wide layout omitted used/total capacity")
		}
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 4 {
			t.Fatal("mount list not bounded")
		}
	}
	if !reflect.DeepEqual(data.Filesystems, wantOrder) {
		t.Fatal("rendering mutated agent mount order")
	}
}

func TestUnavailableAndPartialFilesystems(t *testing.T) {
	for _, filesystem := range []client.Filesystem{
		{MountPoint: "/bad"},
		{MountPoint: "/bad", TotalBytes: 100, UsedBytes: 101},
		{MountPoint: "/bad", TotalBytes: 100, AvailableBytes: 101},
		{MountPoint: "/bad", TotalBytes: 100, UsedPercent: -1},
		{MountPoint: "/bad", TotalBytes: 100, UsedPercent: math.NaN()},
		{MountPoint: "/bad", TotalBytes: 100, UsedPercent: math.Inf(1)},
	} {
		data := client.MetricsData{Filesystems: []client.Filesystem{filesystem, {MountPoint: "/good", TotalBytes: 100, UsedBytes: 25, UsedPercent: 25}}, Errors: []client.MetricError{{Metric: "FILESYSTEMS"}}}
		view := ansi.Strip(FilesystemSummary(data, 80))
		for _, want := range []string{"partial", "/good", "25.0%", "/bad", "unavailable"} {
			if !strings.Contains(view, want) {
				t.Fatalf("partial response missing %q: %q", want, view)
			}
		}
	}
	if FilesystemSummary(client.MetricsData{}, 80) != "" {
		t.Fatal("legacy response displays a fake filesystem list")
	}
	if !strings.Contains(FilesystemSummary(client.MetricsData{Errors: []client.MetricError{{Metric: "filesystems"}}}, 80), "unavailable") {
		t.Fatal("missing failed list not marked unavailable")
	}
}

func TestPartialHeadingIncludesUnreadableMountCount(t *testing.T) {
	data := client.MetricsData{
		Filesystems: []client.Filesystem{{MountPoint: "/", TotalBytes: 100, UsedBytes: 50, UsedPercent: 50}},
		Errors: []client.MetricError{
			{Metric: "filesystems", Message: `mount "/run/user/0": permission denied`},
			{Metric: "FILESYSTEMS", Message: `mount "/var/lib/docker/rootfs/overlay": permission denied`},
			{Metric: "filesystems", Message: "mount table unavailable"},
		},
	}
	view := ansi.Strip(FilesystemSummary(data, 80))
	if !strings.Contains(view, "FILESYSTEMS · partial · 2 mounts unavailable") {
		t.Fatalf("missing compact error count: %q", view)
	}
	if strings.Contains(view, "/run/user") || strings.Contains(view, "/var/lib/docker") {
		t.Fatal("mount deny details leaked into summary")
	}
	if height := lipgloss.Height(FilesystemSummary(data, 80)); height != 2 {
		t.Fatalf("partial list uses %d lines, want heading and mount", height)
	}
}

func TestLongAndUnsafeMountPaths(t *testing.T) {
	data := client.MetricsData{Filesystems: []client.Filesystem{{MountPoint: "\x1b[31m/data\x1b[0m\n\t" + strings.Repeat("界", 100), TotalBytes: 1024, UsedPercent: 50}}}
	for _, width := range []int{24, 40, 80} {
		view := FilesystemSummary(data, width)
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 2 || strings.Contains(view, "\x1b[31m") {
			t.Fatal("mount path affected terminal layout or style")
		}
	}
}
