package styles

import "charm.land/lipgloss/v2"

var (
	Title    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dd3fc"))
	Subtitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	Panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#334155")).Padding(0, 1)
	Label    = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	Value    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	Good     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	Bad      = lipgloss.NewStyle().Foreground(lipgloss.Color("#fb7185"))
	Warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24"))
	Footer   = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
)
