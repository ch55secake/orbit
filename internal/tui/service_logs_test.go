package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestServiceLogsOpenHistoryFollowPauseAndPageOlder(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model.services = &serviceBrowser{page: client.ServicePage{Units: serviceUnits()}}
	var queries []client.ServiceLogQuery
	model.serviceLogLoader = func(_ context.Context, _ config.Server, unit string, query client.ServiceLogQuery) (client.ServiceLogPage, error) {
		if unit != "a.service" {
			t.Errorf("requested alias %q, want inventory identity", unit)
		}
		queries = append(queries, query)
		switch {
		case query.Before != "":
			return client.ServiceLogPage{Unit: "canonical.service", Entries: []client.ServiceLogEntry{testServiceLogEntry("old", "older history")}}, nil
		case query.After != "":
			return client.ServiceLogPage{Unit: "canonical.service", Entries: []client.ServiceLogEntry{}}, nil
		default:
			return client.ServiceLogPage{
				Unit: "canonical.service",
				Entries: []client.ServiceLogEntry{
					testServiceLogEntry("current-1", "\x1b[31mready\x1b[0m\nworker café ✓"),
					testServiceLogEntry("current-2", "second entry"),
				},
				NextCursor: "older-cursor", LatestCursor: "current-2", HasMore: true,
			}, nil
		}
	}
	_, command := model.Update(keyPress("g"))
	if command == nil {
		t.Fatal("g did not open service logs")
	}
	_, _ = model.Update(command())
	if model.services.logs == nil || model.services.logs.canonical != "canonical.service" || model.services.logs.latestCursor != "current-2" || !model.services.logs.hasOlder {
		t.Fatalf("recent log page was not installed: %+v", model.services.logs)
	}
	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "SERVICE LOGS") || !strings.Contains(view, "canonical.service") || !strings.Contains(view, "worker café ✓") || strings.Contains(model.View().Content, "\x1b[31m") {
		t.Fatalf("log view missing identity/content or failed sanitization:\n%s", view)
	}
	_, command = model.Update(keyPress("pgup"))
	if command == nil {
		t.Fatal("PageUp did not request older history")
	}
	_, _ = model.Update(command())
	if len(queries) != 2 || queries[1].Before != "older-cursor" || len(model.services.logs.entries) != 3 || model.services.logs.entries[0].Cursor != "old" {
		t.Fatalf("older-page request failed: queries=%+v entries=%+v", queries, model.services.logs.entries)
	}
	if model.services.logs.latestCursor != "current-2" {
		t.Fatal("older-page response moved the follow cursor backwards")
	}
	_, command = model.Update(keyPress("f"))
	if command == nil {
		t.Fatal("follow did not start a bounded poll")
	}
	_, pollTimer := model.Update(command())
	if len(queries) != 3 || queries[2].After != "current-2" || queries[2].Before != "" {
		t.Fatalf("follow did not use the latest opaque cursor: %+v", queries)
	}
	if pollTimer == nil || !model.services.logs.following {
		t.Fatal("follow did not schedule the next poll")
	}
	_, _ = model.Update(keyPress("p"))
	if !model.services.logs.paused {
		t.Fatal("pause key did not pause follow")
	}
	message := pollTimer()
	if poll, ok := message.(serviceLogsPollMsg); !ok || !poll.canceled {
		t.Fatalf("pause did not cancel the scheduled poll: %#v", message)
	}
	model.Update(message)
	if model.services.logs.loading {
		t.Fatal("canceled poll launched another request")
	}
}

func TestServiceLogsExpiredCursorKeepsHistoryAndRefreshRecovers(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.services = &serviceBrowser{page: client.ServicePage{Units: serviceUnits()}}
	page := client.ServiceLogPage{Unit: "canonical.service", Entries: []client.ServiceLogEntry{testServiceLogEntry("cursor", "visible history")}, LatestCursor: "cursor"}
	failPoll := false
	model.serviceLogLoader = func(_ context.Context, _ config.Server, _ string, query client.ServiceLogQuery) (client.ServiceLogPage, error) {
		if failPoll && query.After != "" {
			return client.ServiceLogPage{}, &client.ServiceError{Code: "log_cursor_expired", Status: 410}
		}
		return page, nil
	}
	_, command := model.Update(keyPress("g"))
	_, _ = model.Update(command())
	failPoll = true
	_, command = model.Update(keyPress("f"))
	_, nextTimer := model.Update(command())
	if model.services.logs.following || model.services.logs.hasOlder || !strings.Contains(model.services.logs.err, "cursor expired") {
		t.Fatal("expired cursor was not surfaced and stopped following")
	}
	if len(model.services.logs.entries) != 1 || model.services.logs.entries[0].Message != "visible history" {
		t.Fatal("expired cursor silently discarded retained history")
	}
	if nextTimer != nil {
		t.Fatal("expired cursor unexpectedly scheduled another poll")
	}
	failPoll = false
	_, command = model.Update(keyPress("r"))
	_, _ = model.Update(command())
	if model.services.logs.err != "" || model.services.logs.latestCursor != "cursor" {
		t.Fatal("refresh did not recover from expired cursor")
	}
}

func TestServiceLogsWarnWhenFollowStartsWithoutAnAnchorCursor(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.services = &serviceBrowser{logs: &serviceLogState{
		unit: client.ServiceUnit{Name: "worker.service"}, server: "alpha", following: true,
	}}
	command := model.receiveServiceLogs(serviceLogsMsg{
		kind: "poll",
		page: client.ServiceLogPage{
			Unit: "worker.service", Entries: []client.ServiceLogEntry{testServiceLogEntry("latest", "new log")},
			LatestCursor: "latest", HasMore: true,
		},
	})
	if command == nil || !model.services.logs.trimmed || model.services.logs.latestCursor != "latest" {
		t.Fatal("bounded first follow page without a cursor did not report omitted history")
	}
	model.closeServices()
}

func TestServiceLogsCancelAndIgnoreResponsesWhenLeavingOrSwitchingServers(t *testing.T) {
	for _, key := range []string{"esc", "tab"} {
		t.Run(key, func(t *testing.T) {
			model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
			model.services = &serviceBrowser{page: client.ServicePage{Units: serviceUnits()}}
			model.serviceLoader = func(_ context.Context, target config.Server, _ client.ServiceQuery, _ string) (client.ServicePage, *client.ServiceUnit, error) {
				return client.ServicePage{Units: []client.ServiceUnit{{Name: target.Name + ".service"}}}, nil, nil
			}
			started := make(chan struct{})
			var requestContext context.Context
			model.serviceLogLoader = func(ctx context.Context, _ config.Server, _ string, _ client.ServiceLogQuery) (client.ServiceLogPage, error) {
				requestContext = ctx
				close(started)
				<-ctx.Done()
				return client.ServiceLogPage{}, ctx.Err()
			}
			_, command := model.Update(keyPress("g"))
			result := make(chan tea.Msg, 1)
			go func() { result <- command() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("log request did not start")
			}
			_, next := model.Update(keyPress(key))
			if key == "tab" {
				if requestContext.Err() == nil || next == nil {
					t.Fatal("server switch did not cancel logs and load the new server")
				}
				finishServiceCommand(t, model, next)
				if model.table.Cursor() != 1 || model.services.page.Units[0].Name != "beta.service" {
					t.Fatal("server switch displayed the wrong service inventory")
				}
			} else if model.services == nil || model.services.logs != nil || requestContext.Err() == nil {
				t.Fatal("leaving the log view did not cancel the in-flight request")
			}
			select {
			case stale := <-result:
				model.Update(stale)
			case <-time.After(time.Second):
				t.Fatal("canceled log request did not return")
			}
			if key == "esc" && (model.services == nil || model.services.logs != nil) {
				t.Fatal("stale response reopened the log view")
			}
		})
	}
}

func TestServiceLogBufferBoundsEntryCountAndMessageBytes(t *testing.T) {
	entries := make([]client.ServiceLogEntry, maxServiceLogEntries+25)
	for index := range entries {
		entries[index] = testServiceLogEntry(fmt.Sprintf("cursor-%03d", index), "message")
	}
	bounded, dropped, trimmed := boundServiceLogEntries(entries, false)
	if len(bounded) != maxServiceLogEntries || len(dropped) != 25 || !trimmed || bounded[0].Cursor != "cursor-025" {
		t.Fatalf("entry-count bound failed: retained=%d dropped=%d trimmed=%t", len(bounded), len(dropped), trimmed)
	}
	large := []client.ServiceLogEntry{
		testServiceLogEntry("large-1", strings.Repeat("a", maxServiceLogBytes/2)),
		testServiceLogEntry("large-2", strings.Repeat("b", maxServiceLogBytes/2)),
		testServiceLogEntry("large-3", strings.Repeat("c", maxServiceLogBytes/2)),
	}
	bounded, dropped, trimmed = boundServiceLogEntries(large, false)
	retainedBytes := 0
	for _, entry := range bounded {
		retainedBytes += len(entry.Message) + len(entry.Cursor)
	}
	if retainedBytes > maxServiceLogBytes || len(dropped) == 0 || !trimmed {
		t.Fatalf("byte bound failed: bytes=%d dropped=%d trimmed=%t", retainedBytes, len(dropped), trimmed)
	}
}

func testServiceLogEntry(cursor, message string) client.ServiceLogEntry {
	return client.ServiceLogEntry{Timestamp: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Message: message, Cursor: cursor}
}
