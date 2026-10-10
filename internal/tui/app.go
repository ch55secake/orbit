package tui

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/views"
)

type app struct {
	version    string
	configPath string
	cfg        config.Config
	apiClient  *client.Client
	table      table.Model
	statuses   []server.Status
	loading    bool
	message    string
	width      int
}

type metricsMsg struct {
	statuses []server.Status
}

func Run(configPath, version string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	model := newApp(cfg, configPath, version)
	program := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	_, err = program.Run()
	return err
}

func newApp(cfg config.Config, configPath, version string) *app {
	rows := makeRows(cfg.Servers, nil)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Foreground(lipgloss.Color("#7dd3fc"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#1e3a5f"))
	model := &app{
		version:    version,
		configPath: configPath,
		cfg:        cfg,
		apiClient:  client.New(5 * time.Second),
		statuses:   make([]server.Status, len(cfg.Servers)),
		table: table.New(
			table.WithColumns([]table.Column{
				{Title: "SERVER", Width: 20},
				{Title: "STATUS", Width: 12},
				{Title: "CPU", Width: 10},
				{Title: "MEMORY", Width: 12},
				{Title: "DISK", Width: 12},
				{Title: "UPTIME", Width: 12},
			}),
			table.WithRows(rows),
			table.WithFocused(true),
			table.WithHeight(6),
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.message = ""
			return m, m.fetchMetrics()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.table.SetWidth(max(msg.Width-4, 60))
		m.table.SetHeight(max(msg.Height/3, 6))
	case metricsMsg:
		m.statuses = msg.statuses
		m.loading = false
		m.message = ""
		for _, status := range m.statuses {
			if status.Error != nil {
				m.message = "One or more servers could not be reached"
				break
			}
		}
		m.table.SetRows(makeRows(m.cfg.Servers, m.statuses))
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *app) View() tea.View {
	var selected *server.Status
	if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.statuses) {
		selected = &m.statuses[cursor]
	}
	view := tea.NewView(views.Render(views.DashboardData{
		Version:    m.version,
		ConfigPath: m.configPath,
		Table:      m.table.View(),
		Selected:   selected,
		Loading:    m.loading,
		Message:    m.message,
	}))
	view.AltScreen = true
	return view
}

func (m *app) fetchMetrics() tea.Cmd {
	targets := append([]config.Server(nil), m.cfg.Servers...)
	apiClient := m.apiClient
	return func() tea.Msg {
		statuses := make([]server.Status, len(targets))
		var waitGroup sync.WaitGroup
		for i, target := range targets {
			waitGroup.Add(1)
			go func(index int, target config.Server) {
				defer waitGroup.Done()
				statuses[index].Server = target
				metrics, err := apiClient.Metrics(context.Background(), target.Address)
				if err != nil {
					statuses[index].Error = err
					return
				}
				statuses[index].Metrics = &metrics
			}(i, target)
		}
		waitGroup.Wait()
		return metricsMsg{statuses: statuses}
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
				row[2] = fmt.Sprintf("%.1f%%", data.CPU.UsagePercent)
				row[3] = fmt.Sprintf("%.1f%%", data.Memory.UsedPercent)
				row[4] = fmt.Sprintf("%.1f%%", data.Disk.UsedPercent)
				row[5] = client.FormatUptime(data.Uptime)
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
