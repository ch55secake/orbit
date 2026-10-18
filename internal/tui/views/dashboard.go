package views

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/styles"
	"github.com/charmbracelet/x/ansi"
)

type DashboardData struct {
	Version        string
	ConfigPath     string
	Table          string
	Statuses       []server.Status
	Selected       *server.Status
	SelectedIndex  int
	Empty          bool
	Loading        bool
	Message        string
	Width          int
	Height         int
	ShowHelp       bool
	PasswordPrompt bool
	PasswordServer string
	PasswordInput  string
	UpdateConfirm  bool
	UpdateServer   string
	Notice         string
	NoticeError    bool
	CanUpdate      bool
	ShellActive    bool
	ShellServer    string
	ShellContent   string
	ShellStatus    string
}

func Render(data DashboardData) string {
	if data.ShellActive && !TerminalBounds(data).Fits() {
		return renderTerminalResizeWarning(data)
	}
	terminalWidth := data.Width
	if data.Width > 0 {
		data.Width = max(data.Width-4, 1)
	}
	header := renderMasthead(data)
	summary := fleetSummary(data)
	groupedFleet := FleetInMasthead(terminalWidth, data.Height)
	if !groupedFleet {
		header = lipgloss.JoinVertical(lipgloss.Left, header, summary)
	}
	footer := renderMenu(data)
	help := ""
	if data.ShowHelp && !data.PasswordPrompt && !data.UpdateConfirm && !data.ShellActive {
		help = styles.Subtitle.Render("CONFIG  " + data.ConfigPath + "\n↑/k ↓/j navigate · pgup/pgdown page · home/end first/last\ns SSH shell · u update Stardust · r refresh · q quit · ? close help")
		if data.Width > 0 {
			help = ansi.Wrap(help, data.Width, "")
		}
	}
	bodyHeight := data.Height - 4 - lipgloss.Height(header) - lipgloss.Height(footer)
	if help != "" {
		bodyHeight -= lipgloss.Height(help) + 1
	}
	detailWidth := data.Width
	if terminalWidth >= 110 {
		detailWidth = data.Width - TableWidth(terminalWidth) - 3
	}

	var detail string
	sideTelemetry := ""
	switch {
	case data.Loading && (data.Selected == nil || data.Selected.Metrics == nil):
		detail = styles.Warn.Render("Loading metrics…")
	case data.Empty:
		detail = lipgloss.JoinVertical(lipgloss.Left,
			styles.Subtitle.Render("No servers configured"),
			styles.Label.Render("Add a server with: orbit servers add"),
		)
	case data.Selected == nil:
		detail = styles.Subtitle.Render("Select a server to inspect its metrics")
	case data.Selected.Error != nil:
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Bad.Render("● offline")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, styles.Bad.Render(data.Selected.Error.Error()), styles.Label.Render("Press r to retry the connection"))
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	case data.Selected.Metrics != nil:
		metrics := data.Selected.Metrics.Data
		lines := []string{styles.Title.Render(data.Selected.Server.Name) + "  " + styles.Good.Render("● online")}
		if status := tunnelStatus(data.Selected); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, styles.Subtitle.Render(metrics.Hostname+" · "+metrics.OS.Name+" "+metrics.OS.PlatformVersion), "")
		filesystems := components.FilesystemSummary(metrics, detailWidth)
		interfaces := components.InterfaceSummary(metrics, detailWidth)
		cpuDetails := components.CPUDetails(metrics, detailWidth)
		telemetry := ""
		cardHeight := 3
		if expanded(data) && detailWidth >= 56 && !data.ShowHelp {
			telemetry = networkDetails(data.Selected, detailWidth)
			if groupedFleet {
				if !data.PasswordPrompt {
					sideTelemetry = networkDetails(data.Selected, TableWidth(terminalWidth))
				}
				telemetry = ""
			}
			available := bodyHeight - lipgloss.Height(lipgloss.JoinVertical(lipgloss.Left, lines...)) - 1
			if telemetry != "" {
				available -= lipgloss.Height(telemetry) + 1
			}
			if terminalWidth < 110 {
				available -= lipgloss.Height(data.Table) + 1
			}
			available -= len(metrics.Errors)
			if swap := components.SwapSummary(metrics); swap != "" {
				available -= lipgloss.Height(ansi.Wrap(swap, detailWidth, ""))
			}
			if data.Message != "" {
				available -= 2
			}
			if data.Notice != "" {
				available -= 1 + lipgloss.Height(ansi.Wrap(data.Notice, detailWidth, ""))
			}
			if available < 6 && telemetry != "" {
				available += lipgloss.Height(telemetry) + 1
				telemetry = ""
			}
			cardHeight = available / 2
		}
		if filesystems != "" || interfaces != "" || cpuDetails != "" {
			available := bodyHeight - lipgloss.Height(lipgloss.JoinVertical(lipgloss.Left, lines...)) - len(metrics.Errors)
			if terminalWidth < 110 {
				available -= lipgloss.Height(data.Table) + 1
			}
			if swap := components.SwapSummary(metrics); swap != "" {
				available -= lipgloss.Height(ansi.Wrap(swap, detailWidth, ""))
			}
			if telemetry != "" {
				available -= lipgloss.Height(telemetry) + 1
			}
			if data.Message != "" {
				available -= 2
			}
			if data.Notice != "" {
				available -= lipgloss.Height(ansi.Wrap(data.Notice, detailWidth, "")) + 1
			}
			extraHeight := func() int {
				height := 0
				for _, content := range []string{cpuDetails, filesystems, interfaces} {
					if content != "" {
						height += lipgloss.Height(content) + 1
					}
				}
				return height
			}
			minimum := lipgloss.Height(ansi.Wrap(components.Summary(metrics), detailWidth, ""))
			if swap := components.SwapSummary(metrics); swap != "" {
				minimum -= lipgloss.Height(ansi.Wrap(swap, detailWidth, ""))
			}
			if available-extraHeight() < minimum && telemetry != "" {
				available += lipgloss.Height(telemetry) + 1
				telemetry = ""
			}
			if (cpuDetails != "" || filesystems != "") && interfaces != "" && available-extraHeight() < minimum {
				interfaces = styles.Subtitle.Render(ansi.Truncate(fmt.Sprintf("Interfaces: %d (resize)", len(metrics.Network.Interfaces)), detailWidth, "…"))
			}
			if (cpuDetails != "" || filesystems != "") && interfaces != "" && available-extraHeight() < minimum {
				interfaces = ""
			}
			if filesystems != "" && available-extraHeight() < minimum {
				// Keep the fullest mount visible alongside newer CPU detail when
				// there is no room for the full bounded filesystem list.
				prefix, suffix := "FILESYSTEMS: ", ""
				if len(metrics.Filesystems) > 1 {
					suffix = fmt.Sprintf(" (+%d)", len(metrics.Filesystems)-1)
				}
				rows := strings.Split(components.FilesystemSummary(metrics, max(detailWidth-len(prefix)-len(suffix), 1)), "\n")
				if len(rows) > 1 {
					filesystems = styles.Label.Render(prefix) + rows[1] + styles.Subtitle.Render(suffix)
				}
			}
			cardHeight = min(cardHeight, (available-extraHeight()-1)/2)
		}
		if cardHeight < 3 {
			lines = append(lines, components.Summary(metrics))
		} else {
			lines = append(lines, components.SizedCards(metrics, detailWidth, cardHeight))
		}
		if telemetry != "" {
			lines = append(lines, "", telemetry)
		}
		if cpuDetails != "" {
			lines = append(lines, "", cpuDetails)
		}
		if filesystems != "" {
			lines = append(lines, "", filesystems)
		}
		if interfaces != "" {
			lines = append(lines, "", interfaces)
		}
		for _, err := range metrics.Errors {
			lines = append(lines, styles.Warn.Render(err.Metric+": "+err.Message))
		}
		detail = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	if data.Message != "" {
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, "", styles.Warn.Render(data.Message))
	}
	if data.Notice != "" {
		style := styles.Good
		if data.NoticeError {
			style = styles.Bad
		}
		detail = lipgloss.JoinVertical(lipgloss.Left, detail, "", style.Render(data.Notice))
	}
	if detail == "" {
		detail = styles.Subtitle.Render("Waiting for metrics…")
	}
	if data.ShellActive {
		detail = renderTerminal(data, detailWidth)
	} else if data.PasswordPrompt {
		detail = renderPanel(lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render("SSH password required: "+data.PasswordServer),
			styles.Subtitle.Render("Used for this connection only; never saved."),
			"",
			"Password: "+data.PasswordInput,
		), detailWidth)
	} else if data.UpdateConfirm {
		detail = renderPanel(lipgloss.JoinVertical(lipgloss.Left,
			styles.Title.Render("Update Stardust on "+data.UpdateServer+"?"),
			styles.Value.Render("Install the latest published release and restart the service."),
			styles.Subtitle.Render("Existing service settings are preserved; root/sudo is required."),
			"", styles.Warn.Render("Press y to confirm. Enter or Esc cancels."),
		), detailWidth)
	} else if detailWidth > 0 {
		detail = ansi.Wrap(detail, detailWidth, "")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, data.Table, "", detail)
	if terminalWidth >= 110 {
		list := data.Table
		if groupedFleet {
			list = hostDetails(data.Selected, TableWidth(terminalWidth))
			if sideTelemetry != "" && bodyHeight >= lipgloss.Height(list)+lipgloss.Height(sideTelemetry)+1 {
				list = lipgloss.JoinVertical(lipgloss.Left,
					lipgloss.NewStyle().Height(bodyHeight-lipgloss.Height(sideTelemetry)-1).Render(list), "", sideTelemetry,
				)
			}
		} else if expanded(data) && !data.ShowHelp && !data.PasswordPrompt {
			info := hostDetails(data.Selected, TableWidth(terminalWidth))
			if info != "" && bodyHeight >= lipgloss.Height(list)+lipgloss.Height(info)+1 {
				list = lipgloss.JoinVertical(lipgloss.Left,
					lipgloss.NewStyle().Height(bodyHeight-lipgloss.Height(info)-1).Render(list), "", info,
				)
			}
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			fleetPane(list, TableWidth(terminalWidth), bodyHeight, data.ShellActive),
			"   ",
			detail,
		)
	}
	if bodyHeight > lipgloss.Height(body) {
		body = lipgloss.NewStyle().Height(bodyHeight).Render(body)
	}
	sections := []string{header, "", body, "", footer}
	if help != "" {
		sections = append(sections, "", help)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func fleetPane(content string, width, height int, shellActive bool) string {
	style := lipgloss.NewStyle().Width(width)
	if shellActive {
		style = style.MaxHeight(max(height, 1))
	}
	return style.Render(content)
}

func hostDetails(status *server.Status, width int) string {
	if status == nil {
		return ""
	}
	var lines []string
	if status.Metrics != nil {
		data := status.Metrics.Data
		lines = append(lines, styles.Label.Render("HOST"), styles.Title.Render(data.Hostname),
			styles.Subtitle.Render(strings.TrimSpace(data.OS.Name+" "+data.OS.PlatformVersion+" "+data.OS.KernelArch)), "")
	}
	lines = append(lines, styles.Label.Render("LINK"))
	// Only show the origin: userinfo, paths, and query strings can contain secrets.
	if address, err := url.Parse(status.Server.Address); err == nil && address.Host != "" {
		lines = append(lines, styles.Value.Render(address.Scheme+"://"+address.Host))
	}
	if ssh := status.Server.SSH; ssh != nil {
		peer := ssh.Host
		if ssh.Port > 0 {
			peer = net.JoinHostPort(ssh.Host, strconv.Itoa(ssh.Port))
		}
		if ssh.User != "" {
			peer = ssh.User + "@" + peer
		}
		lines = append(lines, styles.Subtitle.Render("SSH  "+peer), tunnelStatus(status))
	} else {
		lines = append(lines, styles.Subtitle.Render("Direct HTTP connection"))
	}
	return ansi.Wrap(lipgloss.JoinVertical(lipgloss.Left, lines...), width, "")
}

func networkDetails(status *server.Status, width int) string {
	data := status.Metrics.Data
	lines := []string{styles.Label.Render("NETWORK · cumulative counters")}
	failed := false
	for _, err := range data.Errors {
		if strings.EqualFold(err.Metric, "network") {
			failed = true
			break
		}
	}
	if failed {
		lines = append(lines, styles.Warn.Render("— unavailable"))
	} else {
		lines = append(lines,
			components.Metric("RX", fmt.Sprintf("%s · %d packets", client.FormatBytes(data.Network.BytesReceived), data.Network.PacketsReceived)),
			components.Metric("TX", fmt.Sprintf("%s · %d packets", client.FormatBytes(data.Network.BytesSent), data.Network.PacketsSent)),
		)
	}
	if !status.Metrics.Timestamp.IsZero() {
		lines = append(lines, styles.Subtitle.Render("Sample  "+status.Metrics.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")))
	}
	return ansi.Wrap(lipgloss.JoinVertical(lipgloss.Left, lines...), width, "")
}

// TableWidth is shared with the model so layout and column sizing stay in sync.
func TableWidth(terminalWidth int) int {
	if terminalWidth >= 110 {
		return min(50, max(38, (terminalWidth-4)/3))
	}
	return max(terminalWidth-4, 1)
}

func renderPanel(content string, width int) string {
	if width <= 0 {
		return styles.Panel.Render(content)
	}
	inner := max(width-4, 1)
	return styles.Panel.Width(width).Render(ansi.Wrap(content, inner, ""))
}

func fleetSummary(data DashboardData) string {
	online, offline := 0, 0
	for _, status := range data.Statuses {
		if status.Error != nil {
			offline++
		} else if status.Metrics != nil {
			online++
		}
	}
	parts := []string{styles.Label.Render(fmt.Sprintf("%d servers", len(data.Statuses)))}
	if online > 0 {
		parts = append(parts, styles.Good.Render(fmt.Sprintf("%d online", online)))
	}
	if offline > 0 {
		parts = append(parts, styles.Bad.Render(fmt.Sprintf("%d offline", offline)))
	}
	if data.Loading {
		parts = append(parts, styles.Warn.Render("Loading metrics…"))
	} else if pending := len(data.Statuses) - online - offline; pending > 0 {
		parts = append(parts, styles.Subtitle.Render(fmt.Sprintf("%d pending", pending)))
	}
	if data.Selected != nil && data.SelectedIndex >= 0 && data.SelectedIndex < len(data.Statuses) {
		parts = append(parts, styles.Subtitle.Render(fmt.Sprintf("selected %d/%d", data.SelectedIndex+1, len(data.Statuses))))
	}
	summary := strings.Join(parts, "   ")
	if data.Width > 0 {
		return ansi.Wrap(summary, data.Width, "")
	}
	return summary
}

func renderMenu(data DashboardData) string {
	if data.ShellActive {
		return fitMenuHints([]string{menuHint("ctrl+]", "close SSH")}, data.Width)
	}
	if data.PasswordPrompt {
		hints := []string{
			menuHint("enter", "submit"),
			menuHint("esc", "cancel"),
			menuHint("ctrl+c", "quit"),
		}
		return fitMenuHints(hints, data.Width)
	}
	if data.UpdateConfirm {
		return fitMenuHints([]string{menuHint("y", "update"), menuHint("esc / n", "cancel"), menuHint("ctrl+c", "quit")}, data.Width)
	}
	if data.Empty {
		return fitMenuHints([]string{menuHint("?", "help"), menuHint("q", "quit")}, data.Width)
	}
	upLabel, downLabel := "previous", "next"
	if data.Width > 0 && data.Width < 60 {
		upLabel, downLabel = "up", "down"
	}
	hints := []string{
		menuHint("↑ k", upLabel),
		menuHint("↓ j", downLabel),
	}
	if !data.Loading {
		hints = append(hints, menuHint("r", "refresh"))
	}
	if data.Selected != nil && data.Selected.Server.SSH != nil {
		hints = append(hints, menuHint("s", "SSH"))
		if data.CanUpdate {
			hints = append(hints, menuHint("u", "update"))
		}
	}
	hints = append(hints, menuHint("?", "help"), menuHint("q", "quit"))
	return fitMenuHints(hints, data.Width)
}

func fitMenuHints(hints []string, width int) string {
	if width <= 0 {
		return joinMenuHints(hints)
	}
	var lines []string
	line := ""
	for _, hint := range hints {
		if line != "" && lipgloss.Width(line)+3+lipgloss.Width(hint) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += hint
	}
	lines = append(lines, line)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func joinMenuHints(hints []string) string {
	items := make([]string, 0, len(hints)*2-1)
	for i, hint := range hints {
		if i > 0 {
			items = append(items, "   ")
		}
		items = append(items, hint)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, items...)
}

func menuHint(key, label string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center,
		styles.Key.Render(key),
		" ",
		styles.Footer.Render(label),
	)
}

func tunnelStatus(status *server.Status) string {
	if status.Server.SSH == nil {
		return ""
	}
	state := status.TunnelState
	if state == "" {
		state = "unknown"
	}
	return styles.Subtitle.Render("SSH tunnel: " + state)
}
