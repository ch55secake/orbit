package tui

import (
	"context"
	"errors"
	"os"
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
	"github.com/ch55secake/orbit/internal/tui/components"
	"github.com/ch55secake/orbit/internal/tui/views"
	"github.com/ch55secake/orbit/internal/tunnel"
)

type app struct {
	version          string
	configPath       string
	ctx              context.Context
	cfg              config.Config
	apiClient        metricsClient
	tunnels          server.TunnelManager
	table            table.Model
	statuses         []server.Status
	loading          bool
	message          string
	width            int
	height           int
	showHelp         bool
	program          *tea.Program
	passwordRequest  *passwordRequest
	passwordInput    []byte
	passwordPromptMu sync.Mutex
}

type metricsMsg struct {
	statuses []server.Status
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
		version:    version,
		configPath: configPath,
		cfg:        cfg,
		ctx:        context.Background(),
		apiClient:  client.New(5 * time.Second),
		statuses:   make([]server.Status, len(cfg.Servers)),
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
	case passwordRequestMsg:
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "r":
			if m.loading {
				return m, nil
			}
			m.loading = true
			m.message = ""
			return m, m.fetchMetrics()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(views.TableWidth(msg.Width))
		m.table.SetColumns(tableColumns(views.TableWidth(msg.Width)))
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
	return m, cmd
}

func (m *app) resizeTableHeight() {
	if m.height <= 0 {
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
	m.table.SetHeight(min(max(len(m.cfg.Servers)+1, 2), max(m.height-reserved, 2)))
}

func (m *app) View() tea.View {
	var selected *server.Status
	var passwordServer string
	if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.statuses) {
		selected = &m.statuses[cursor]
	}
	if m.passwordRequest != nil {
		passwordServer = m.passwordRequest.server.Name
	}
	view := tea.NewView(views.Render(views.DashboardData{
		Version:        m.version,
		ConfigPath:     m.configPath,
		Table:          m.table.View(),
		Statuses:       m.statuses,
		Selected:       selected,
		Empty:          len(m.cfg.Servers) == 0,
		Loading:        m.loading,
		Message:        m.message,
		Width:          m.width,
		Height:         m.height,
		ShowHelp:       m.showHelp,
		PasswordPrompt: m.passwordRequest != nil,
		PasswordServer: passwordServer,
		PasswordInput:  strings.Repeat("•", utf8.RuneCount(m.passwordInput)),
	}))
	view.AltScreen = true
	return view
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
