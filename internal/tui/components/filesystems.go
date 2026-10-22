package components

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

// FilesystemSummary prioritizes full mounts without changing the agent's order
// or letting large mount lists crowd out CPU, memory, disk, and uptime.
func FilesystemSummary(data client.MetricsData, width int) string {
	failed := metricFailed(data, "filesystems")
	card := width >= 56
	contentWidth := width
	if card {
		contentWidth -= 2
	}
	if len(data.Filesystems) == 0 {
		if failed {
			if card {
				return detailCard(width, styles.Label.Render("FILESYSTEMS"), styles.Warn.Render("— unavailable"))
			}
			return styles.Label.Render("FILESYSTEMS") + "\n" + styles.Warn.Render("— unavailable")
		}
		return ""
	}
	contentWidth = max(contentWidth, 1)
	filesystems := sortedFilesystems(data.Filesystems)
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
	lines := []string{styles.Label.Render(ansi.Truncate(heading, contentWidth, "…"))}
	const limit = 2
	for _, filesystem := range filesystems[:min(len(filesystems), limit)] {
		path := filesystemPath(filesystem.MountPoint)
		usage := "— unavailable"
		if filesystemAvailable(filesystem) {
			usage = Percent(filesystem.UsedPercent)
			if card {
				usage += fmt.Sprintf("  %s / %s", client.FormatBytes(filesystem.UsedBytes), client.FormatBytes(filesystem.TotalBytes))
			}
		}
		path = ansi.Truncate(path, max(contentWidth-ansi.StringWidth(usage)-2, 1), "…")
		lines = append(lines, styles.Value.Render(ansi.Truncate(path+"  "+usage, contentWidth, "…")))
		if card && filesystemAvailable(filesystem) {
			lines = append(lines, usageBar(filesystem.UsedPercent, min(contentWidth, 24), styles.Card.GetBackground()))
		}
	}
	if len(filesystems) > limit {
		remaining := len(filesystems) - limit
		label := "mounts"
		if remaining == 1 {
			label = "mount"
		}
		lines = append(lines, styles.Subtitle.Render(ansi.Truncate(fmt.Sprintf("+%d more %s", remaining, label), contentWidth, "…")))
	}
	if card {
		return detailCard(width, lines...)
	}
	return strings.Join(lines, "\n")
}

// FilesystemCompact keeps the fullest mount and a short meter on one row when
// the dashboard cannot fit the full filesystem card.
func FilesystemCompact(data client.MetricsData, width int) string {
	width = max(width, 1)
	if len(data.Filesystems) == 0 {
		if metricFailed(data, "filesystems") {
			return ansi.Truncate(Metric("FILESYSTEMS", "— unavailable"), width, "…")
		}
		return ""
	}
	filesystems := sortedFilesystems(data.Filesystems)
	filesystem := filesystems[0]
	prefix := styles.Label.Render("FILESYSTEMS: ")
	usage := "— unavailable"
	if filesystemAvailable(filesystem) {
		usage = Percent(filesystem.UsedPercent)
	}
	suffix := ""
	if len(filesystems) > 1 {
		suffix = fmt.Sprintf(" (+%d)", len(filesystems)-1)
	}
	bar := ""
	if filesystemAvailable(filesystem) && width >= 32 {
		barWidth := min(10, max(width-lipgloss.Width(prefix)-lipgloss.Width(usage+suffix)-5, 1))
		bar = " " + usageBar(filesystem.UsedPercent, barWidth, nil)
	}
	pathWidth := max(width-lipgloss.Width(prefix)-lipgloss.Width(usage+bar+suffix)-2, 1)
	path := ansi.Truncate(filesystemPath(filesystem.MountPoint), pathWidth, "…")
	return ansi.Truncate(prefix+styles.Value.Render(path+" "+usage)+bar+styles.Subtitle.Render(suffix), width, "…")
}

func sortedFilesystems(filesystems []client.Filesystem) []client.Filesystem {
	sorted := slices.Clone(filesystems)
	slices.SortStableFunc(sorted, func(left, right client.Filesystem) int {
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
	return sorted
}

func filesystemPath(path string) string {
	path = strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, ansi.Strip(path)))
	if path == "" {
		return "(unknown mount)"
	}
	return path
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
