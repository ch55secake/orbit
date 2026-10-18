package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/tui/views"
)

const (
	maxServiceLogEntries = 500
	maxServiceLogBytes   = 2 * 1024 * 1024
	serviceLogPollEvery  = time.Second
)

type serviceLogState struct {
	unit          client.ServiceUnit
	server        string
	canonical     string
	entries       []client.ServiceLogEntry
	latestCursor  string
	olderCursor   string
	hasOlder      bool
	following     bool
	paused        bool
	loading       bool
	trimmed       bool
	err           string
	scroll        int
	generation    uint64
	retryDelay    time.Duration
	requestCancel context.CancelFunc
	pollCancel    context.CancelFunc
}

func (l *serviceLogState) displayUnit() string {
	if l.canonical != "" {
		return l.canonical
	}
	if l.unit.ID != "" {
		return l.unit.ID
	}
	return l.unit.Name
}

type serviceLogsMsg struct {
	generation uint64
	kind       string
	page       client.ServiceLogPage
	err        error
}

type serviceLogsPollMsg struct {
	generation uint64
	canceled   bool
}

func (m *app) openServiceLogs() tea.Cmd {
	browser := m.services
	if browser == nil || browser.logs != nil || browser.loading || len(browser.page.Units) == 0 {
		return nil
	}
	selected := min(max(browser.cursor, 0), len(browser.page.Units)-1)
	unit := browser.page.Units[selected]
	index := m.table.Cursor()
	if index < 0 || index >= len(m.cfg.Servers) {
		return nil
	}
	browser.logs = &serviceLogState{
		unit: unit, server: m.cfg.Servers[index].Name, loading: true, scroll: -1, retryDelay: serviceLogPollEvery,
	}
	return m.loadServiceLogPage("recent")
}

func (m *app) closeServiceLogs() {
	if m.services == nil || m.services.logs == nil {
		return
	}
	logs := m.services.logs
	if logs.requestCancel != nil {
		logs.requestCancel()
	}
	if logs.pollCancel != nil {
		logs.pollCancel()
	}
	m.serviceLogGeneration++
	m.services.logs = nil
}

func (m *app) cancelServiceLogWork(logs *serviceLogState) {
	if logs.requestCancel != nil {
		logs.requestCancel()
		logs.requestCancel = nil
	}
	if logs.pollCancel != nil {
		logs.pollCancel()
		logs.pollCancel = nil
	}
	logs.loading = false
	m.serviceLogGeneration++
	logs.generation = m.serviceLogGeneration
}

func (m *app) loadServiceLogPage(kind string) tea.Cmd {
	if m.services == nil || m.services.logs == nil {
		return nil
	}
	logs := m.services.logs
	if kind == "older" && (!logs.hasOlder || logs.olderCursor == "" || logs.loading) {
		return nil
	}
	m.cancelServiceLogWork(logs)
	ctx, cancel := context.WithCancel(m.ctx)
	logs.requestCancel = cancel
	logs.loading, logs.err = true, ""
	generation := logs.generation
	index := m.table.Cursor()
	if index < 0 || index >= len(m.cfg.Servers) {
		cancel()
		logs.loading = false
		return nil
	}
	target := m.cfg.Servers[index]
	unit := logs.unit.Name
	query := client.ServiceLogQuery{Limit: client.MaxServiceLogEntries}
	switch kind {
	case "older":
		query.Before = logs.olderCursor
	case "poll":
		query.After = logs.latestCursor
	}
	loader := m.serviceLogLoader
	if loader == nil {
		tunnels, prompt := m.tunnels, m.requestPassword
		loader = func(ctx context.Context, target config.Server, unit string, query client.ServiceLogQuery) (client.ServiceLogPage, error) {
			return server.ReadServiceLogs(ctx, target, tunnels, prompt, unit, query)
		}
	}
	return func() tea.Msg {
		defer cancel()
		page, err := loader(ctx, target, unit, query)
		return serviceLogsMsg{generation: generation, kind: kind, page: page, err: err}
	}
}

func (m *app) receiveServiceLogs(msg serviceLogsMsg) tea.Cmd {
	if m.services == nil || m.services.logs == nil || msg.generation != m.services.logs.generation {
		return nil
	}
	logs := m.services.logs
	logs.loading = false
	if logs.requestCancel != nil {
		logs.requestCancel()
		logs.requestCancel = nil
	}
	if msg.err != nil {
		logs.err = client.ServiceText(msg.err.Error())
		if isExpiredServiceLogCursor(msg.err) {
			logs.hasOlder = false
			if msg.kind == "older" {
				logs.trimmed = true
				if logs.following && !logs.paused {
					return m.scheduleServiceLogPoll(logs.retryDelay)
				}
			} else {
				logs.following, logs.paused = false, false
			}
			return nil
		}
		if logs.following && !logs.paused {
			if fatalServiceLogError(msg.err) {
				logs.following = false
				return nil
			}
			logs.retryDelay = nextServiceLogRetry(logs.retryDelay)
			return m.scheduleServiceLogPoll(logs.retryDelay)
		}
		return nil
	}
	logs.err = ""
	logs.retryDelay = serviceLogPollEvery
	if msg.page.Unit != "" {
		logs.canonical = msg.page.Unit
	}
	switch msg.kind {
	case "recent":
		if msg.page.LatestCursor != "" {
			logs.latestCursor = msg.page.LatestCursor
		}
		logs.entries, logs.trimmed = append([]client.ServiceLogEntry(nil), msg.page.Entries...), false
		logs.olderCursor = msg.page.NextCursor
		logs.hasOlder = msg.page.NextCursor != ""
		logs.scroll = -1
		logs.entries, _, logs.trimmed = boundServiceLogEntries(logs.entries, false)
	case "older":
		newEntries := uniqueServiceLogEntries(msg.page.Entries, logs.entries)
		oldScroll := logs.scroll
		combined := append(newEntries, logs.entries...)
		wasTrimmed := logs.trimmed
		var dropped []client.ServiceLogEntry
		var bounded bool
		logs.entries, dropped, bounded = boundServiceLogEntries(combined, true)
		logs.trimmed = wasTrimmed || bounded
		logs.olderCursor = msg.page.NextCursor
		logs.hasOlder = msg.page.NextCursor != ""
		if oldScroll > 0 {
			logs.scroll += views.ServiceLogLineCount(newEntries, views.ServiceLogsContentWidth(m.width))
		}
		if len(dropped) > 0 {
			logs.trimmed = true
		}
	case "poll":
		hadCursor := logs.latestCursor != ""
		if msg.page.LatestCursor != "" {
			logs.latestCursor = msg.page.LatestCursor
		}
		if !hadCursor && msg.page.HasMore {
			logs.trimmed = true
		}
		atBottom := logs.scroll < 0 || logs.scroll >= m.serviceLogMaxScroll(logs)
		newEntries := uniqueServiceLogEntries(msg.page.Entries, logs.entries)
		combined := append(append([]client.ServiceLogEntry(nil), logs.entries...), newEntries...)
		wasTrimmed := logs.trimmed
		var dropped []client.ServiceLogEntry
		var bounded bool
		logs.entries, dropped, bounded = boundServiceLogEntries(combined, false)
		logs.trimmed = wasTrimmed || bounded
		if len(dropped) > 0 {
			logs.trimmed = true
			if !atBottom && logs.scroll >= 0 {
				logs.scroll = max(logs.scroll-views.ServiceLogLineCount(dropped, views.ServiceLogsContentWidth(m.width)), 0)
			}
		}
		if atBottom {
			logs.scroll = -1
		}
	}
	if logs.following && !logs.paused {
		delay := logs.retryDelay
		if msg.kind == "poll" && msg.page.HasMore {
			delay = 0
		}
		return m.scheduleServiceLogPoll(delay)
	}
	return nil
}

func (m *app) scheduleServiceLogPoll(delay time.Duration) tea.Cmd {
	if m.services == nil || m.services.logs == nil {
		return nil
	}
	logs := m.services.logs
	if logs.pollCancel != nil {
		logs.pollCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	logs.pollCancel = cancel
	generation := logs.generation
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return serviceLogsPollMsg{generation: generation, canceled: true}
		case <-timer.C:
			return serviceLogsPollMsg{generation: generation}
		}
	}
}

func (m *app) receiveServiceLogsPoll(msg serviceLogsPollMsg) tea.Cmd {
	if msg.canceled || m.services == nil || m.services.logs == nil {
		return nil
	}
	logs := m.services.logs
	if msg.generation != logs.generation || !logs.following || logs.paused || logs.loading {
		return nil
	}
	if logs.pollCancel != nil {
		logs.pollCancel()
		logs.pollCancel = nil
	}
	return m.loadServiceLogPage("poll")
}

func (m *app) serviceLogKeys(key tea.KeyPressMsg) tea.Cmd {
	if m.services == nil || m.services.logs == nil {
		return nil
	}
	logs := m.services.logs
	switch key.String() {
	case "ctrl+c":
		m.closeServices()
		return tea.Quit
	case "esc":
		m.closeServiceLogs()
		return nil
	case "tab", "shift+tab":
		return m.switchServiceServer(key)
	case "r":
		logs.entries, logs.latestCursor, logs.olderCursor = nil, "", ""
		logs.hasOlder, logs.trimmed = false, false
		logs.scroll = -1
		return m.loadServiceLogPage("recent")
	case "f":
		if logs.following {
			logs.following, logs.paused = false, false
			m.cancelServiceLogWork(logs)
			return nil
		}
		logs.following, logs.paused = true, false
		logs.scroll = -1
		if !logs.loading {
			return m.loadServiceLogPage("poll")
		}
		return nil
	case "p", " ", "space":
		if !logs.following {
			return nil
		}
		if logs.paused {
			logs.paused = false
			if !logs.loading {
				return m.loadServiceLogPage("poll")
			}
			return nil
		}
		logs.paused = true
		m.cancelServiceLogWork(logs)
		return nil
	case "up", "k":
		logs.scroll = max(m.serviceLogCurrentTop(logs)-1, 0)
	case "down", "j":
		logs.scroll = min(m.serviceLogCurrentTop(logs)+1, m.serviceLogMaxScroll(logs))
	case "home":
		logs.scroll = 0
	case "end":
		logs.scroll = -1
	case "pgup":
		logs.scroll = max(m.serviceLogCurrentTop(logs)-views.ServiceLogsViewport(m.height), 0)
		if logs.scroll == 0 && logs.hasOlder && !logs.loading {
			return m.loadServiceLogPage("older")
		}
	case "pgdown":
		logs.scroll = min(m.serviceLogCurrentTop(logs)+views.ServiceLogsViewport(m.height), m.serviceLogMaxScroll(logs))
	}
	return nil
}

func (m *app) serviceLogCurrentTop(logs *serviceLogState) int {
	if logs.scroll < 0 {
		return m.serviceLogMaxScroll(logs)
	}
	return min(max(logs.scroll, 0), m.serviceLogMaxScroll(logs))
}

func (m *app) serviceLogMaxScroll(logs *serviceLogState) int {
	width := views.ServiceLogsContentWidth(m.width)
	total := views.ServiceLogLineCount(logs.entries, width)
	return max(total-views.ServiceLogsViewport(m.height), 0)
}

func uniqueServiceLogEntries(entries, existing []client.ServiceLogEntry) []client.ServiceLogEntry {
	seen := make(map[string]struct{}, len(existing)+len(entries))
	for _, entry := range existing {
		seen[entry.Cursor] = struct{}{}
	}
	unique := make([]client.ServiceLogEntry, 0, len(entries))
	for _, entry := range entries {
		if _, found := seen[entry.Cursor]; found {
			continue
		}
		seen[entry.Cursor] = struct{}{}
		unique = append(unique, entry)
	}
	return unique
}

func boundServiceLogEntries(entries []client.ServiceLogEntry, keepOldest bool) ([]client.ServiceLogEntry, []client.ServiceLogEntry, bool) {
	bytes := 0
	for _, entry := range entries {
		bytes += len(entry.Message) + len(entry.Cursor)
	}
	dropped := make([]client.ServiceLogEntry, 0)
	for len(entries) > maxServiceLogEntries || bytes > maxServiceLogBytes {
		index := 0
		if keepOldest {
			index = len(entries) - 1
		}
		bytes -= len(entries[index].Message) + len(entries[index].Cursor)
		dropped = append(dropped, entries[index])
		entries = append(entries[:index], entries[index+1:]...)
	}
	return entries, dropped, len(dropped) > 0
}

func isExpiredServiceLogCursor(err error) bool {
	var serviceErr *client.ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == "log_cursor_expired"
}

func fatalServiceLogError(err error) bool {
	var serviceErr *client.ServiceError
	if !errors.As(err, &serviceErr) {
		return false
	}
	switch serviceErr.Code {
	case "log_cursor_expired", "logs_disabled", "logs_unauthorized", "logs_unsupported", "log_unit_forbidden", "log_unit_not_found", "logs_permission_denied", "invalid_log_query", "invalid_unit":
		return true
	default:
		return serviceErr.Status >= 400 && serviceErr.Status < 500 && serviceErr.Status != 408 && serviceErr.Status != 429
	}
}

func nextServiceLogRetry(delay time.Duration) time.Duration {
	if delay < serviceLogPollEvery {
		delay = serviceLogPollEvery
	} else if delay < 15*time.Second {
		delay *= 2
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}
