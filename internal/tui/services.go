package tui

import (
	"context"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
)

type serviceLoader func(context.Context, config.Server, client.ServiceQuery, string) (client.ServicePage, *client.ServiceUnit, error)

type serviceBrowser struct {
	page     client.ServicePage
	cursor   int
	query    client.ServiceQuery
	previous []string
	input    string
	editing  bool
	loading  bool
	err      string
	detail   *client.ServiceUnit
	cancel   context.CancelFunc
}

type servicesMsg struct {
	generation uint64
	page       client.ServicePage
	detail     *client.ServiceUnit
	err        error
}

func (m *app) closeServices() {
	if m.services != nil && m.services.cancel != nil {
		m.services.cancel()
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
	browser.loading, browser.err, browser.detail = true, "", nil
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
		return
	}
	browser.page, browser.cursor = msg.page, 0
}

func (m *app) serviceKeys(key tea.KeyPressMsg) tea.Cmd {
	browser := m.services
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
	case "/":
		browser.editing, browser.input = true, browser.query.Filter
	case "r":
		unit := ""
		if browser.detail != nil {
			unit = browser.detail.Name
		}
		return m.loadServices(unit)
	case "enter":
		if !browser.loading && len(browser.page.Units) > 0 {
			return m.loadServices(browser.page.Units[browser.cursor].Name)
		}
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
