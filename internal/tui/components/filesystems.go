package components

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// FilesystemSummary prioritizes full mounts without changing the agent's order
// or letting large mount lists crowd out CPU, memory, disk, and uptime.
func FilesystemSummary(data client.MetricsData, width int) string {
	failed := metricFailed(data, "filesystems")
	if len(data.Filesystems) == 0 {
		if failed {
			return styles.Label.Render("FILESYSTEMS") + "\n" + styles.Warn.Render("— unavailable")
		}
		return ""
	}
	width = max(width, 1)
	filesystems := slices.Clone(data.Filesystems)
	slices.SortStableFunc(filesystems, func(left, right client.Filesystem) int {
		leftValid, rightValid := filesystemAvailable(left), filesystemAvailable(right)
		if leftValid != rightValid {
			if leftValid {
				return -1
			}
			return 1
		}
		if !leftValid {
			return 0
		}
		return cmp.Compare(right.UsedPercent, left.UsedPercent)
	})
	heading := "FILESYSTEMS · fullest first"
	if failed {
		heading = "FILESYSTEMS · partial"
		if unavailable := unavailableMountCount(data.Errors); unavailable > 0 {
			mountLabel := "mounts"
			if unavailable == 1 {
				mountLabel = "mount"
			}
			heading = fmt.Sprintf("FILESYSTEMS · partial · %d %s unavailable", unavailable, mountLabel)
		}
	}
	lines := []string{styles.Label.Render(ansi.Truncate(heading, width, "…"))}
	const limit = 2
	for _, filesystem := range filesystems[:min(len(filesystems), limit)] {
		path := strings.TrimSpace(strings.Map(func(character rune) rune {
			if unicode.IsControl(character) {
				return ' '
			}
			return character
		}, ansi.Strip(filesystem.MountPoint)))
		if path == "" {
			path = "(unknown mount)"
		}
		usage := "— unavailable"
		if filesystemAvailable(filesystem) {
			usage = Percent(filesystem.UsedPercent)
			if width >= 56 {
				usage += fmt.Sprintf("  %s / %s", client.FormatBytes(filesystem.UsedBytes), client.FormatBytes(filesystem.TotalBytes))
			}
		}
		path = ansi.Truncate(path, max(width-ansi.StringWidth(usage)-2, 1), "…")
		lines = append(lines, styles.Value.Render(ansi.Truncate(path+"  "+usage, width, "…")))
	}
	if len(filesystems) > limit {
		remaining := len(filesystems) - limit
		label := "mounts"
		if remaining == 1 {
			label = "mount"
		}
		lines = append(lines, styles.Subtitle.Render(ansi.Truncate(fmt.Sprintf("+%d more %s", remaining, label), width, "…")))
	}
	return strings.Join(lines, "\n")
}

func unavailableMountCount(failures []client.MetricError) int {
	count := 0
	for _, failure := range failures {
		if strings.EqualFold(failure.Metric, "filesystems") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(failure.Message)), "mount \"") {
			count++
		}
	}
	return count
}

func filesystemAvailable(filesystem client.Filesystem) bool {
	return filesystem.TotalBytes > 0 && filesystem.UsedBytes <= filesystem.TotalBytes && filesystem.AvailableBytes <= filesystem.TotalBytes && validPercent(filesystem.UsedPercent)
}
