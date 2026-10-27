package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/shell"
	"github.com/ch55secake/orbit/internal/sshclient"
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/views"
	"github.com/ch55secake/orbit/internal/tunnel"
	"github.com/ch55secake/orbit/internal/update"
	uv "github.com/charmbracelet/ultraviolet"
)

type app struct {
	version              string
	configPath           string
	ctx                  context.Context
	cfg                  config.Config
	apiClient            metricsClient
	tunnels              server.TunnelManager
	table                table.Model
	statuses             []server.Status
	loading              bool
	message              string
	width                int
	height               int
	showHelp             bool
	program              *tea.Program
	passwordRequest      *passwordRequest
	passwordInput        []byte
	passwordPromptMu     sync.Mutex
	updateTarget         *config.Server
	updateCommand        func(context.Context, config.Server, string) (*exec.Cmd, error)
	updateServer         string
	updateRunning        bool
	shellUpdate          bool
	notice               string
	noticeError          bool
	shellSession         *shell.Session
	shellServer          string
	shellStatus          string
	shellBounds          views.TerminalGeometry
	shellSelection       *shellSelection
	services             *serviceBrowser
	serviceLoader        serviceLoader
	serviceGeneration    uint64
	serviceLogLoader     serviceLogLoader
	serviceLogGeneration uint64
}

type metricsMsg struct {
	statuses []server.Status
}

type shellEventMsg struct{ session *shell.Session }

type updateEndedMsg struct {
	server string
	err    error
}

type passwordRequest struct {
	server   config.Server
	response chan passwordResponse
}

type passwordRequestMsg struct {
	request *passwordRequest
}

type passwordResponse struct {
	password []byte
	err      error
}

type metricsClient interface {
	Metrics(context.Context, string) (client.MetricsResponse, error)
}

func Run(configPath, version string) (runErr error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	tunnels := tunnel.NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		runErr = errors.Join(runErr, tunnels.Close())
	}()
	model := newApp(cfg, configPath, version)
	defer model.closeShell()
	defer model.closeServices()
	model.ctx = ctx
	model.tunnels = tunnels
	program := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model.program = program
	_, err = program.Run()
	return err
}

func newApp(cfg config.Config, configPath, version string) *app {
	rows := makeRows(cfg.Servers, nil)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(false).Foreground(lipgloss.Color("#8993a5"))
	styles.Selected = styles.Selected.Bold(false).Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#293047"))
	model := &app{
		version:       version,
		configPath:    configPath,
		cfg:           cfg,
		updateCommand: update.Command,
		ctx:           context.Background(),
		apiClient:     client.New(5 * time.Second),
		statuses:      make([]server.Status, len(cfg.Servers)),
		table: table.New(
			table.WithColumns(tableColumns(90)),
			table.WithRows(rows),
			table.WithFocused(true),
			table.WithHeight(min(max(len(cfg.Servers)+1, 2), 7)),
			table.WithStyles(styles),
		),
	}
	for i, target := range cfg.Servers {
		model.statuses[i].Server = target
	}
	return model
}

func (m *app) Init() tea.Cmd {
	if len(m.cfg.Servers) == 0 {
		return nil
	}
	m.loading = true
	return m.fetchMetrics()
}

func (m *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case servicesMsg:
		m.receiveServices(msg)
		return m, nil
	case serviceLogsMsg:
		return m, m.receiveServiceLogs(msg)
	case serviceLogsPollMsg:
		return m, m.receiveServiceLogsPoll(msg)
	case tea.MouseMsg:
		m.selectShell(msg)
		return m, nil
	case shellEventMsg:
		if msg.session != m.shellSession {
			return m, nil
		}
		if exited, err := m.shellSession.Result(); exited {
			if m.updateRunning {
				m.updateRunning = false
				m.shellStatus = "update complete · Ctrl+] to close"
				if err != nil {
					m.shellStatus = "update failed · Ctrl+] to close"
				}
				server := m.updateServer
				return m, func() tea.Msg { return updateEndedMsg{server: server, err: err} }
			}
			m.shellStatus = "exited · ctrl+] to close"
			if err != nil {
				m.shellStatus = err.Error() + " · ctrl+] to close"
			}
			return m, nil
		}
		return m, waitShell(m.shellSession)
	case tea.PasteMsg:
		if m.services != nil && m.services.editing && m.passwordRequest == nil {
			m.services.appendFilter(msg.Content)
			return m, nil
		}
		if m.shellSession != nil {
			m.shellSelection = nil
			if views.TerminalBounds(m.dashboardData()).Fits() {
				m.shellSession.Paste(msg.Content)
			}
			return m, nil
		}
	case updateEndedMsg:
		m.noticeError = msg.err != nil
		m.notice = "Stardust updated on " + msg.server
		if msg.err != nil {
			m.notice = "Stardust update failed on " + msg.server + ": " + msg.err.Error()
		}
		m.loading = true
		m.resizeTableHeight()
		return m, m.fetchMetrics()
	case passwordRequestMsg:
		m.updateTarget = nil
		if m.passwordRequest != nil {
			m.passwordRequest.response <- passwordResponse{err: errors.New("SSH password prompt was interrupted")}
		}
		m.passwordRequest = msg.request
		clear(m.passwordInput)
		m.passwordInput = nil
		return m, nil
	case tea.KeyPressMsg:
		if m.passwordRequest != nil {
			return m, m.updatePasswordInput(msg)
		}
		if m.updateTarget != nil {
			return m, m.updateConfirmation(msg)
		}
		if m.shellSession != nil {
			if m.shellSelection != nil {
				switch msg.String() {
				case "ctrl+y", "ctrl+shift+c":
					text := m.shellSelection.content(false)
					if text != "" {
						return m, tea.SetClipboard(text)
					}
					return m, nil
				case "esc":
					m.shellSelection = nil
					return m, nil
				}
			}
			m.shellSelection = nil
			key := uv.KeyPressEvent(msg).Key()
			if key.Mod == uv.ModShift {
				switch key.Code {
				case uv.KeyPgUp:
					m.shellSession.Scroll(max(m.shellBounds.Height-1, 1))
					return m, nil
				case uv.KeyPgDown:
					m.shellSession.Scroll(-max(m.shellBounds.Height-1, 1))
					return m, nil
				}
			}
			if msg.String() == "ctrl+]" {
				if m.updateRunning {
					m.shellStatus = "update running · wait for completion"
					return m, nil
				}
				m.closeShell()
				m.resizeTableHeight()
				return m, nil
			}
			if views.TerminalBounds(m.dashboardData()).Fits() {
				m.shellSession.SendKey(uv.KeyPressEvent(msg))
			}
			return m, nil
		}
		if m.services != nil {
			return m, m.serviceKeys(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "v":
			if m.loading {
				m.message = "Wait for metrics loading to finish before browsing services"
				return m, nil
			}
			if len(m.cfg.Servers) == 0 {
				return m, nil
			}
			m.showHelp = false
			m.services = &serviceBrowser{}
			return m, m.loadServices("")
		case "s":
			if m.loading {
				m.message = "Wait for metrics loading to finish before opening SSH"
				return m, nil
			}
			cursor := m.table.Cursor()
			if cursor < 0 || cursor >= len(m.cfg.Servers) {
				return m, nil
			}
			process, err := sshclient.Command(m.ctx, m.cfg.Servers[cursor], true, "")
			if err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.showHelp = false
			m.shellBounds = views.TerminalBounds(m.dashboardData())
			if !m.shellBounds.Fits() {
				m.message = "Terminal is too small for embedded SSH"
				return m, nil
			}
			session, err := shell.Start(process, m.shellBounds.Width, m.shellBounds.Height)
			if err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.shellSession = session
			m.shellServer = m.cfg.Servers[cursor].Name
			m.shellStatus = ""
			m.message = ""
			return m, waitShell(session)
		case "u":
			if m.loading {
				m.message = "Wait for metrics loading to finish before updating Stardust"
				return m, nil
			}
			cursor := m.table.Cursor()
			if cursor < 0 || cursor >= len(m.cfg.Servers) {
				return m, nil
			}
			target := m.cfg.Servers[cursor]
			if err := update.Validate(target, "latest"); err != nil {
				m.message = err.Error()
				return m, nil
			}
			settings := *target.SSH
			target.SSH = &settings
			m.updateTarget = &target
			m.showHelp = false
			return m, nil
		case "r":
			if m.loading {
				return m, nil
			}
			m.loading = true
			m.message = ""
			m.notice = ""
			return m, m.fetchMetrics()
		}
	case tea.WindowSizeMsg:
		m.shellSelection = nil
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(views.FleetWidth(msg.Width, msg.Height))
		m.table.SetColumns(tableColumns(views.FleetWidth(msg.Width, msg.Height)))
	case metricsMsg:
		m.statuses = msg.statuses
		m.loading = false
		m.message = ""
		for _, status := range m.statuses {
			if status.Error != nil {
				m.message = "One or more servers could not be reached"
				break
			}
			if status.Metrics != nil && len(status.Metrics.Data.Errors) > 0 {
				m.message = "Some metrics are unavailable"
			}
		}
		m.table.SetRows(makeRows(m.cfg.Servers, m.statuses))
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	m.resizeTableHeight()
	if m.shellSession != nil {
		bounds := views.TerminalBounds(m.dashboardData())
		if bounds.Fits() && bounds != m.shellBounds {
			m.shellBounds = bounds
			_ = m.shellSession.Resize(bounds.Width, bounds.Height)
		}
	}
	return m, cmd
}

func (m *app) updateConfirmation(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "n", "N", "enter":
		m.updateTarget = nil
	case "ctrl+c":
		m.updateTarget = nil
		return tea.Quit
	case "y", "Y":
		target := *m.updateTarget
		m.updateTarget = nil
		process, err := m.updateCommand(m.ctx, target, "latest")
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.notice = ""
		m.noticeError = false
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			return tea.ExecProcess(process, func(err error) tea.Msg {
				return updateEndedMsg{server: target.Name, err: err}
			})
		}
		bounds := views.TerminalBounds(m.dashboardData())
		if !bounds.Fits() {
			m.message = "Terminal is too small to run the update inside Orbit"
			return nil
		}
		session, err := shell.Start(process, bounds.Width, bounds.Height)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.shellSession = session
		m.shellServer = target.Name
		m.shellStatus = ""
		m.shellBounds = bounds
		m.shellUpdate = true
		m.updateRunning = true
		m.updateServer = target.Name
		m.showHelp = false
		m.message = ""
		return waitShell(session)
	}
	return nil
}

func (m *app) resizeTableHeight() {
	if m.height <= 0 {
		return
	}
	if views.FleetInMasthead(m.width, m.height) {
		m.table.SetHeight(min(max(len(m.cfg.Servers)+1, 2), 10))
		return
	}
	// Stacked layouts reserve space below the list; split layouts use it beside
	// the list. Recalculate on selection/metrics changes as warnings need room.
	reserved := 20
	if m.width >= 110 {
		reserved = 9
	}
	if m.width >= 64 && m.height >= 32 {
		reserved = 29
		if m.width >= 110 {
			reserved = 15
		} else {
			if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.statuses) && m.statuses[cursor].Metrics != nil {
				reserved += len(m.statuses[cursor].Metrics.Data.Errors)
			}
			if m.message != "" {
				reserved += 2
			}
		}
	}
	if m.width >= 64 && m.height >= 40 {
		reserved += 8 // taller Saturn artwork replaces the seven-line masthead
	}
	if m.notice != "" {
		width := max(m.width-4, 1)
		if m.width >= 110 {
			width -= views.TableWidth(m.width) + 3
		}
		reserved += 1 + lipgloss.Height(lipgloss.NewStyle().Width(width).Render(m.notice))
	}
	if m.width < 110 {
		if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.statuses) && m.statuses[cursor].Metrics != nil {
			if swap := components.SwapDetails(m.statuses[cursor].Metrics.Data, max(m.width-4, 1)); swap != "" {
				reserved += lipgloss.Height(swap)
				if m.width >= 60 {
					reserved++
				}
			}
			if filesystems := components.FilesystemSummary(m.statuses[cursor].Metrics.Data, max(m.width-4, 1)); filesystems != "" {
				reserved += lipgloss.Height(filesystems) + 1
			}
			if interfaces := components.InterfaceSummary(m.statuses[cursor].Metrics.Data, max(m.width-4, 1)); interfaces != "" {
				reserved += lipgloss.Height(interfaces) + 1
			}
			if cpu := components.CPUDetails(m.statuses[cursor].Metrics.Data, max(m.width-4, 1)); cpu != "" {
				reserved += lipgloss.Height(cpu) + 1
			}
		}
	}
	m.table.SetHeight(min(max(len(m.cfg.Servers)+1, 2), max(m.height-reserved, 2)))
}

func (m *app) dashboardData() views.DashboardData {
	var selected *server.Status
	var passwordServer string
	var updateServer string
	if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.statuses) {
		selected = &m.statuses[cursor]
	}
	if m.passwordRequest != nil {
		passwordServer = m.passwordRequest.server.Name
	}
	if m.updateTarget != nil {
		updateServer = m.updateTarget.Name
	}
	shellScrollback := 0
	if m.shellSession != nil {
		shellScrollback = m.shellSession.ScrollOffset()
	}
	data := views.DashboardData{
		Version:         m.version,
		ConfigPath:      m.configPath,
		Table:           m.table.View(),
		Statuses:        m.statuses,
		Selected:        selected,
		SelectedIndex:   m.table.Cursor(),
		Empty:           len(m.cfg.Servers) == 0,
		Loading:         m.loading,
		Message:         m.message,
		Width:           m.width,
		Height:          m.height,
		ShowHelp:        m.showHelp,
		PasswordPrompt:  m.passwordRequest != nil,
		PasswordServer:  passwordServer,
		PasswordInput:   strings.Repeat("•", utf8.RuneCount(m.passwordInput)),
		UpdateConfirm:   m.updateTarget != nil,
		UpdateServer:    updateServer,
		Notice:          m.notice,
		NoticeError:     m.noticeError,
		CanUpdate:       selected != nil && !m.loading && update.Validate(selected.Server, "latest") == nil,
		ShellActive:     m.shellSession != nil,
		ShellUpdate:     m.shellUpdate,
		UpdateRunning:   m.updateRunning,
		ShellScrollback: shellScrollback,
		ShellServer:     m.shellServer,
		ShellStatus:     m.shellStatus,
	}
	if m.shellSession != nil {
		data.ShellContent = m.shellSession.Render()
		if m.shellSelection != nil {
			data.ShellContent = m.shellSelection.content(true)
		}
	}
	return data
}

func (m *app) View() tea.View {
	data := m.dashboardData()
	if m.services != nil && m.passwordRequest == nil {
		browser := m.services
		if browser.logs != nil {
			view := tea.NewView(views.RenderServiceLogs(data, views.ServiceLogsData{
				Server: browser.logs.server, Unit: browser.logs.displayUnit(), Entries: browser.logs.entries,
				Scroll: browser.logs.scroll, Loading: browser.logs.loading, Following: browser.logs.following,
				Paused: browser.logs.paused, HasOlder: browser.logs.hasOlder, Trimmed: browser.logs.trimmed,
				Error: browser.logs.err,
			}))
			view.AltScreen = true
			return view
		}
		view := tea.NewView(views.RenderServices(data, views.ServiceBrowserData{Page: browser.page, Cursor: browser.cursor, Filter: browser.query.Filter, Input: browser.input, Editing: browser.editing, Loading: browser.loading, Error: browser.err, Detail: browser.detail, PageNumber: len(browser.previous) + 1}))
		view.AltScreen = true
		return view
	}
	view := tea.NewView(views.Render(data))
	view.AltScreen = true
	if m.shellSession != nil {
		view.MouseMode = tea.MouseModeCellMotion
	}
	if m.shellSession != nil && m.shellSession.ScrollOffset() == 0 && m.shellSelection == nil && m.shellStatus == "" && views.TerminalBounds(data).Fits() {
		position, visible := m.shellSession.Cursor()
		if visible {
			view.Cursor = tea.NewCursor(m.shellBounds.X+position.X, m.shellBounds.Y+position.Y)
		}
	}
	return view
}

func waitShell(session *shell.Session) tea.Cmd {
	return func() tea.Msg { <-session.Events(); return shellEventMsg{session: session} }
}

func (m *app) closeShell() {
	if m.shellSession != nil {
		m.shellSession.Close()
		m.shellSession = nil
	}
	m.shellServer = ""
	m.shellStatus = ""
	m.shellSelection = nil
	m.shellUpdate = false
	m.updateRunning = false
	m.updateServer = ""
}

// Keep every row's six values, but hide secondary columns on smaller screens.
// The selected-server panel always retains the full metrics.
func tableColumns(width int) []table.Column {
	columns := []table.Column{
		{Title: "Server"},
		{Title: "State", Width: 7},
		{Title: "CPU", Width: 7},
		{Title: "Memory", Width: 7},
		{Title: "Disk", Width: 7},
		{Title: "Uptime", Width: 12},
	}
	visible := 6
	switch {
	case width < 36:
		visible = 2
	case width < 56:
		visible = 3
	case width < 76:
		visible = 5
	}
	used := 2 // server cell padding
	for i := 1; i < len(columns); i++ {
		if i >= visible {
			columns[i].Width = 0
		} else {
			used += columns[i].Width + 2
		}
	}
	columns[0].Width = max(width-used, 1)
	return columns
}

func (m *app) fetchMetrics() tea.Cmd {
	targets := append([]config.Server(nil), m.cfg.Servers...)
	apiClient := m.apiClient
	tunnels := m.tunnels
	ctx := m.ctx
	return func() tea.Msg {
		statuses := make([]server.Status, len(targets))
		var waitGroup sync.WaitGroup
		for i, target := range targets {
			waitGroup.Add(1)
			go func(index int, target config.Server) {
				defer waitGroup.Done()
				statuses[index] = server.FetchMetricsWithPrompt(ctx, target, apiClient, tunnels, m.requestPassword)
			}(i, target)
		}
		waitGroup.Wait()
		return metricsMsg{statuses: statuses}
	}
}

func (m *app) requestPassword(ctx context.Context, target config.Server) ([]byte, error) {
	m.passwordPromptMu.Lock()
	defer m.passwordPromptMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.program == nil {
		return nil, errors.New("TUI password prompt is unavailable")
	}
	request := &passwordRequest{server: target, response: make(chan passwordResponse, 1)}
	m.program.Send(passwordRequestMsg{request: request})
	select {
	case response := <-request.response:
		return response.password, response.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *app) updatePasswordInput(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		password := append([]byte(nil), m.passwordInput...)
		clear(m.passwordInput)
		m.passwordInput = nil
		m.passwordRequest.response <- passwordResponse{password: password}
		m.passwordRequest = nil
	case "esc":
		m.cancelPasswordPrompt()
	case "backspace", "ctrl+h":
		if len(m.passwordInput) > 0 {
			_, size := utf8.DecodeLastRune(m.passwordInput)
			for i := len(m.passwordInput) - size; i < len(m.passwordInput); i++ {
				m.passwordInput[i] = 0
			}
			m.passwordInput = m.passwordInput[:len(m.passwordInput)-size]
		}
	case "ctrl+c":
		m.cancelPasswordPrompt()
		return tea.Quit
	default:
		if msg.Text != "" && msg.Mod == 0 {
			m.passwordInput = append(m.passwordInput, msg.Text...)
		}
	}
	return nil
}

func (m *app) cancelPasswordPrompt() {
	clear(m.passwordInput)
	m.passwordInput = nil
	if m.passwordRequest != nil {
		m.passwordRequest.response <- passwordResponse{err: context.Canceled}
		m.passwordRequest = nil
	}
}

func makeRows(targets []config.Server, statuses []server.Status) []table.Row {
	rows := make([]table.Row, len(targets))
	for i, target := range targets {
		row := table.Row{target.Name, "unknown", "—", "—", "—", "—"}
		if i < len(statuses) {
			status := statuses[i]
			switch {
			case status.Error != nil:
				row[1] = "offline"
			case status.Metrics != nil:
				data := status.Metrics.Data
				row[1] = "online"
				values := []string{
					components.Percent(data.CPU.UsagePercent),
					components.Percent(data.Memory.UsedPercent),
					components.Percent(data.Disk.UsedPercent),
					client.FormatUptime(data.Uptime),
				}
				for j, metric := range []string{"cpu", "memory", "disk", "uptime"} {
					if components.MetricAvailable(data, metric) {
						row[j+2] = values[j]
					}
				}
			}
		}
		rows[i] = row
	}
	return rows
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
