package tui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
)

type serviceLoader func(context.Context, config.Server, client.ServiceQuery, string) (client.ServicePage, *client.ServiceUnit, error)
type serviceLogLoader func(context.Context, config.Server, string, client.ServiceLogQuery) (client.ServiceLogPage, error)
type serviceRestartStarter func(context.Context, config.Server, string, string) (client.ServiceRestartOperation, error)
type serviceRestartReader func(context.Context, config.Server, string) (client.ServiceRestartOperation, error)

type serviceBrowser struct {
	page          client.ServicePage
	cursor        int
	query         client.ServiceQuery
	previous      []string
	input         string
	editing       bool
	loading       bool
	err           string
	detail        *client.ServiceUnit
	logs          *serviceLogState
	cancel        context.CancelFunc
	restartNotice string
}

type serviceRestartState struct {
	server     config.Server
	unit       string
	key        string
	phase      string
	operation  client.ServiceRestartOperation
	err        string
	generation uint64
	startedAt  time.Time
	pollDelay  time.Duration
	dismissed  bool
	retrying   bool
}

type servicesMsg struct {
	generation uint64
	page       client.ServicePage
	detail     *client.ServiceUnit
	err        error
}

type serviceRestartStartedMsg struct {
	generation uint64
	operation  client.ServiceRestartOperation
	err        error
}

type serviceRestartPollMsg struct {
	generation uint64
	operation  client.ServiceRestartOperation
	err        error
	canceled   bool
}

func (m *app) closeServices() {
	if m.services != nil && m.services.cancel != nil {
		m.services.cancel()
	}
	if m.services != nil {
		m.closeServiceLogs()
	}
	m.serviceGeneration++
	m.services = nil
}

func (m *app) loadServices(unit string) tea.Cmd {
	browser := m.services
	if browser == nil {
		return nil
	}
	index := m.table.Cursor()
	if index < 0 || index >= len(m.cfg.Servers) {
		return nil
	}
	if browser.cancel != nil {
		browser.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	browser.cancel = cancel
	m.serviceGeneration++
	generation := m.serviceGeneration
	browser.loading, browser.err, browser.detail, browser.restartNotice = true, "", nil, ""
	target, query := m.cfg.Servers[index], browser.query
	loader := m.serviceLoader
	if loader == nil {
		tunnels, prompt := m.tunnels, m.requestPassword
		loader = func(ctx context.Context, target config.Server, query client.ServiceQuery, unit string) (client.ServicePage, *client.ServiceUnit, error) {
			return server.ReadServices(ctx, target, tunnels, prompt, query, unit)
		}
	}
	return func() tea.Msg {
		defer cancel()
		page, detail, err := loader(ctx, target, query, unit)
		return servicesMsg{generation: generation, page: page, detail: detail, err: err}
	}
}

func (m *app) receiveServices(msg servicesMsg) {
	if m.services == nil || msg.generation != m.serviceGeneration {
		return
	}
	browser := m.services
	browser.loading = false
	if msg.err != nil {
		browser.err = client.ServiceText(msg.err.Error())
		return
	}
	if msg.detail != nil {
		browser.detail = msg.detail
		browser.page.Capabilities.Restart = msg.detail.RestartCapability
		return
	}
	browser.page, browser.cursor = msg.page, 0
}

func (m *app) serviceKeys(key tea.KeyPressMsg) tea.Cmd {
	browser := m.services
	if browser.logs != nil {
		return m.serviceLogKeys(key)
	}
	if key.String() == "ctrl+c" {
		m.closeServices()
		return tea.Quit
	}
	if browser.editing {
		switch key.String() {
		case "esc":
			browser.editing = false
			browser.input = browser.query.Filter
		case "enter":
			browser.editing = false
			browser.query.Filter, browser.query.Cursor = browser.input, ""
			browser.previous = nil
			return m.loadServices("")
		case "backspace", "ctrl+h":
			if browser.input != "" {
				_, size := utf8.DecodeLastRuneInString(browser.input)
				browser.input = browser.input[:len(browser.input)-size]
			}
		default:
			if key.Mod == 0 {
				browser.appendFilter(key.Text)
			}
		}
		return nil
	}
	switch key.String() {
	case "esc", "v":
		m.closeServices()
	case "q":
		m.closeServices()
		return tea.Quit
	case "tab", "shift+tab":
		return m.switchServiceServer(key)
	case "/":
		browser.editing, browser.input = true, browser.query.Filter
	case "r":
		unit := ""
		if browser.detail != nil {
			unit = browser.detail.Name
		}
		return m.loadServices(unit)
	case "R":
		return m.requestServiceRestart()
	case "enter":
		if !browser.loading && len(browser.page.Units) > 0 {
			return m.loadServices(browser.page.Units[browser.cursor].Name)
		}
	case "g":
		return m.openServiceLogs()
	case "down", "j", "up", "k", "home", "end":
		if browser.loading || len(browser.page.Units) == 0 {
			return nil
		}
		browser.detail = nil
		switch key.String() {
		case "down", "j":
			browser.cursor = min(browser.cursor+1, len(browser.page.Units)-1)
		case "up", "k":
			browser.cursor = max(browser.cursor-1, 0)
		case "home":
			browser.cursor = 0
		case "end":
			browser.cursor = len(browser.page.Units) - 1
		}
	case "right", "l", "pgdown":
		if browser.loading || browser.page.NextCursor == "" || len(browser.previous) >= 128 {
			return nil
		}
		browser.previous = append(browser.previous, browser.query.Cursor)
		browser.query.Cursor = browser.page.NextCursor
		return m.loadServices("")
	case "left", "h", "pgup":
		if browser.loading || len(browser.previous) == 0 {
			return nil
		}
		browser.query.Cursor = browser.previous[len(browser.previous)-1]
		browser.previous = browser.previous[:len(browser.previous)-1]
		return m.loadServices("")
	}
	return nil
}

func (m *app) requestServiceRestart() tea.Cmd {
	browser := m.services
	if browser == nil || browser.loading || browser.editing {
		return nil
	}
	index := m.table.Cursor()
	if index < 0 || index >= len(m.cfg.Servers) {
		return nil
	}
	var unit client.ServiceUnit
	if browser.detail != nil {
		unit = *browser.detail
	} else if browser.cursor >= 0 && browser.cursor < len(browser.page.Units) {
		unit = browser.page.Units[browser.cursor]
	} else {
		return nil
	}
	if m.serviceRestart != nil {
		state := m.serviceRestart
		if state.phase == "unknown" {
			if state.server.Name != m.cfg.Servers[index].Name || state.unit != unit.Name {
				browser.restartNotice = "A previous restart is unresolved; reconcile that exact server and unit before starting another."
				return nil
			}
			state.dismissed = false
			state.err = ""
			m.notice = ""
			m.noticeError = false
			if state.operation.ID != "" {
				state.phase = "tracking"
				state.startedAt = time.Now()
				state.pollDelay = 0
				return m.scheduleServiceRestartPoll(0)
			}
			return m.submitServiceRestart(true)
		}
		if state.phase != "complete" || !state.dismissed {
			return nil
		}
		m.serviceRestart = nil
	}
	if !browser.page.Capabilities.Restart || !unit.RestartAllowed {
		browser.restartNotice = "This service is read-only; Stardust does not authorize its restart."
		return nil
	}
	target := m.cfg.Servers[index]
	m.serviceRestartSeq++
	m.serviceRestart = &serviceRestartState{
		server: target, unit: unit.Name, phase: "confirm", generation: m.serviceRestartSeq,
	}
	return nil
}

func (m *app) serviceRestartKeys(key tea.KeyPressMsg) tea.Cmd {
	state := m.serviceRestart
	if state == nil {
		return nil
	}
	switch state.phase {
	case "confirm":
		switch key.String() {
		case "y":
			return m.submitServiceRestart(false)
		case "n", "esc":
			m.serviceRestart = nil
		case "ctrl+c":
			return tea.Quit
		}
	case "unknown":
		switch key.String() {
		case "r", "enter":
			m.notice = ""
			m.noticeError = false
			if state.operation.ID != "" {
				state.phase = "tracking"
				state.err = ""
				state.startedAt = time.Now()
				state.pollDelay = 0
				return m.scheduleServiceRestartPoll(0)
			}
			return m.submitServiceRestart(true)
		case "esc":
			state.dismissed = true
			if m.services != nil {
				m.services.restartNotice = state.err
			}
		case "ctrl+c":
			return tea.Quit
		}
	case "complete":
		switch key.String() {
		case "esc", "enter", "space":
			m.serviceRestart = nil
		case "ctrl+c":
			return tea.Quit
		}
	default:
		if key.String() == "ctrl+c" {
			return tea.Quit
		}
	}
	return nil
}

func (m *app) submitServiceRestart(retry bool) tea.Cmd {
	state := m.serviceRestart
	if state == nil {
		return nil
	}
	if !retry {
		key, err := client.NewServiceRestartKey()
		if err != nil {
			state.phase, state.err = "complete", client.ServiceText(err.Error())
			return nil
		}
		state.key = key
	}
	state.phase, state.err, state.dismissed = "submitting", "", false
	m.notice = ""
	m.noticeError = false
	state.retrying = retry
	state.startedAt, state.pollDelay = time.Now(), 250*time.Millisecond
	target, unit, key, generation := state.server, state.unit, state.key, state.generation
	start := m.serviceRestartStart
	if start == nil {
		tunnels, prompt := m.tunnels, m.requestPassword
		start = func(ctx context.Context, target config.Server, unit, key string) (client.ServiceRestartOperation, error) {
			return server.StartServiceRestart(ctx, target, tunnels, prompt, unit, key)
		}
	}
	ctx := m.ctx
	return func() tea.Msg {
		operation, err := start(ctx, target, unit, key)
		return serviceRestartStartedMsg{generation: generation, operation: operation, err: err}
	}
}

func (m *app) receiveServiceRestartStarted(msg serviceRestartStartedMsg) tea.Cmd {
	state := m.serviceRestart
	if state == nil || msg.generation != state.generation {
		return nil
	}
	if msg.err != nil {
		state.err = client.ServiceText(msg.err.Error())
		var serviceErr *client.ServiceError
		if state.retrying || (errors.As(msg.err, &serviceErr) && serviceErr.Code == "restart_transport") {
			state.phase = "unknown"
			m.setServiceRestartUnknownNotice(state)
		} else {
			state.phase = "complete"
		}
		state.retrying = false
		return nil
	}
	if msg.operation.Unit != state.unit {
		state.phase = "unknown"
		state.err = "Stardust returned an operation for a different unit; inspect the service before retrying."
		m.setServiceRestartUnknownNotice(state)
		return nil
	}
	state.retrying = false
	state.operation = msg.operation
	if state.operation.State == client.RestartRunning {
		state.phase, state.pollDelay = "tracking", 250*time.Millisecond
		return m.scheduleServiceRestartPoll(state.pollDelay)
	}
	return m.finishServiceRestart()
}

func (m *app) scheduleServiceRestartPoll(delay time.Duration) tea.Cmd {
	state := m.serviceRestart
	if state == nil || state.operation.ID == "" {
		return nil
	}
	target, operationID, generation := state.server, state.operation.ID, state.generation
	reader := m.serviceRestartStatus
	if reader == nil {
		tunnels, prompt := m.tunnels, m.requestPassword
		reader = func(ctx context.Context, target config.Server, operationID string) (client.ServiceRestartOperation, error) {
			return server.ReadServiceRestartStatus(ctx, target, tunnels, prompt, operationID)
		}
	}
	ctx := m.ctx
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return serviceRestartPollMsg{generation: generation, canceled: true}
		case <-timer.C:
		}
		operation, err := reader(ctx, target, operationID)
		return serviceRestartPollMsg{generation: generation, operation: operation, err: err}
	}
}

func (m *app) receiveServiceRestartPoll(msg serviceRestartPollMsg) tea.Cmd {
	state := m.serviceRestart
	if state == nil || msg.generation != state.generation || msg.canceled {
		return nil
	}
	if msg.err != nil {
		if !client.RetryableRestartStatusError(msg.err) {
			state.phase = "unknown"
			state.err = "Operation status could not be reconciled; use R on the same server and unit to check again. " + client.ServiceText(msg.err.Error())
			m.setServiceRestartUnknownNotice(state)
			return m.loadServices(state.unit)
		}
		if time.Since(state.startedAt) >= 45*time.Second {
			state.phase = "unknown"
			state.err = "Operation status could not be reconciled; use R on the same server and unit to check again. " + client.ServiceText(msg.err.Error())
			m.setServiceRestartUnknownNotice(state)
			return m.loadServices(state.unit)
		}
		state.pollDelay *= 2
		if state.pollDelay < 250*time.Millisecond {
			state.pollDelay = 250 * time.Millisecond
		}
		if state.pollDelay > 3*time.Second {
			state.pollDelay = 3 * time.Second
		}
		return m.scheduleServiceRestartPoll(state.pollDelay)
	}
	if msg.operation.ID != state.operation.ID || msg.operation.Unit != state.unit {
		state.phase = "unknown"
		state.err = "Stardust returned an operation for a different unit; inspect the service before retrying."
		m.setServiceRestartUnknownNotice(state)
		return m.loadServices(state.unit)
	}
	state.operation = msg.operation
	if state.operation.State != client.RestartRunning {
		return m.finishServiceRestart()
	}
	if time.Since(state.startedAt) >= 45*time.Second {
		state.phase = "unknown"
		state.err = "Operation is still running; use R on the same server and unit to reconcile it before any new restart."
		m.setServiceRestartUnknownNotice(state)
		return m.loadServices(state.unit)
	}
	state.pollDelay = 500 * time.Millisecond
	return m.scheduleServiceRestartPoll(state.pollDelay)
}

func (m *app) finishServiceRestart() tea.Cmd {
	state := m.serviceRestart
	if state == nil {
		return nil
	}
	state.phase = "complete"
	state.err = ""
	m.notice = ""
	m.noticeError = false
	if state.operation.State != client.RestartSucceeded {
		state.err = "Restart operation ended in state " + client.ServiceText(state.operation.State) + "; inspect systemd state before retrying."
	}
	if m.services != nil {
		return m.loadServices(state.unit)
	}
	return nil
}

func (m *app) setServiceRestartUnknownNotice(state *serviceRestartState) {
	m.noticeError = true
	m.notice = "Restart outcome unknown for " + client.ServiceText(state.unit) + " on " + client.ServiceText(state.server.Name) + "; reconcile it before starting another restart."
}

func (m *app) switchServiceServer(key tea.KeyPressMsg) tea.Cmd {
	if len(m.cfg.Servers) < 2 {
		return nil
	}
	index := m.table.Cursor() + 1
	if key.String() == "shift+tab" {
		index = m.table.Cursor() + len(m.cfg.Servers) - 1
	}
	m.closeServices()
	m.table.SetCursor(index % len(m.cfg.Servers))
	m.services = &serviceBrowser{}
	return m.loadServices("")
}

func (b *serviceBrowser) appendFilter(text string) {
	for _, r := range client.ServiceText(text) {
		if len(b.input)+utf8.RuneLen(r) > 255 {
			break
		}
		b.input += string(r)
	}
	// Preserve ordinary spaces typed between filter words, while stripping
	// control sequences from bracketed paste and remote text.
	if text == " " && len(b.input) < 255 && !strings.HasSuffix(b.input, " ") {
		b.input += " "
	}
}
