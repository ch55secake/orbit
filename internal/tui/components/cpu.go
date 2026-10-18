package components

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// CPUDetails is additive: it never substitutes load averages for CPU usage.
// Array indices identify logical cores, even when the busiest samples are shown
// first. Null and invalid samples must not appear as idle cores or zero load.
func CPUDetails(data client.MetricsData, width int) string {
	cpu := data.CPU
	if len(cpu.PerCoreUsagePercent) == 0 && cpu.LoadAverage == nil {
		return ""
	}
	if metricFailed(data, "cpu") {
		return ansi.Wrap(Metric("CPU detail", "— unavailable"), max(width, 1), "")
	}
	var lines []string
	if load := cpu.LoadAverage; load != nil {
		lines = append(lines, Metric("Load 1/5/15m", fmt.Sprintf("%s / %s / %s", loadValue(load.OneMinute), loadValue(load.FiveMinutes), loadValue(load.FifteenMinutes))))
	}
	if cores := cpu.PerCoreUsagePercent; len(cores) > 0 {
		indices := make([]int, len(cores))
		for index := range indices {
			indices[index] = index
		}
		slices.SortStableFunc(indices, func(left, right int) int {
			leftValid, rightValid := coreAvailable(cores[left]), coreAvailable(cores[right])
			if leftValid != rightValid {
				if leftValid {
					return -1
				}
				return 1
			}
			if !leftValid {
				return 0
			}
			return cmp.Compare(*cores[right], *cores[left])
		})
		limit := 4
		if width < 56 {
			limit = 2
		}
		var samples []string
		for _, index := range indices[:min(len(indices), limit)] {
			usage := "—"
			if coreAvailable(cores[index]) {
				usage = Percent(*cores[index])
			}
			samples = append(samples, fmt.Sprintf("%d:%s", index, usage))
		}
		if len(cores) > limit {
			samples = append(samples, fmt.Sprintf("+%d more", len(cores)-limit))
		}
		lines = append(lines, Metric("Cores", strings.Join(samples, " ")))
	}
	return ansi.Wrap(strings.Join(lines, "\n"), max(width, 1), "")
}

func coreAvailable(value *float64) bool { return value != nil && validPercent(*value) }

func loadValue(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return "—"
	}
	// Load is not a percentage and may exceed 100; significant digits also keep
	// very large valid samples from consuming hundreds of terminal columns.
	return strconv.FormatFloat(*value, 'g', 4, 64)
}
