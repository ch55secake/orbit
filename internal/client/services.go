package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ch55secake/orbit/internal/credentials"
	"github.com/charmbracelet/x/ansi"
)

type ServiceUnit struct {
	Name          string          `json:"name"`
	ID            string          `json:"id"`
	Description   string          `json:"description"`
	LoadState     string          `json:"load_state"`
	ActiveState   string          `json:"active_state"`
	SubState      string          `json:"sub_state"`
	UnitFileState string          `json:"unit_file_state"`
	MainPID       uint32          `json:"main_pid"`
	MainProcess   *ServiceProcess `json:"main_process,omitempty"`
}

type ServiceProcess struct {
	Name         string `json:"name"`
	EffectiveUID uint32 `json:"effective_uid"`
}

type ServicePage struct {
	Units      []ServiceUnit `json:"units"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type ServiceQuery struct {
	Limit          int
	Filter, Cursor string
}

type ServiceCapabilities struct {
	Enabled        bool   `json:"enabled"`
	Supported      bool   `json:"supported"`
	Authentication string `json:"authentication"`
	Logs           bool   `json:"logs"`
}

type ServiceLogEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	Priority         *int      `json:"priority,omitempty"`
	Message          string    `json:"message"`
	MessageTruncated bool      `json:"message_truncated,omitempty"`
	Cursor           string    `json:"cursor"`
}

type ServiceLogPage struct {
	Unit         string            `json:"unit"`
	Entries      []ServiceLogEntry `json:"entries"`
	NextCursor   string            `json:"next_cursor,omitempty"`
	LatestCursor string            `json:"latest_cursor,omitempty"`
	HasMore      bool              `json:"has_more,omitempty"`
}

type ServiceLogQuery struct {
	Limit  int
	Before string
	After  string
	Since  time.Time
	Until  time.Time
}

const (
	MaxServiceLogEntries      = 100
	MaxServiceLogMessageBytes = 64 * 1024
	MaxServiceLogCursorBytes  = 8192
)

var serviceUnitPattern = regexp.MustCompile(`^[A-Za-z0-9_:@.\\-]+\.service$`)

func ValidServiceUnit(name string) bool {
	return len(name) <= 255 && serviceUnitPattern.MatchString(name) && name != ".service" && !strings.HasPrefix(name, "-") && !strings.Contains(name, "..")
}

// ServiceText neutralizes remote terminal sequences and record separators.
func ServiceText(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, ansi.Strip(text)))
}

// ServiceLogText strips terminal escapes and dangerous controls while preserving
// the readable multiline and Unicode structure of journal messages.
func ServiceLogText(text string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\u200c', '\u200d':
			return r
		case '\u2028', '\u2029':
			return '\n'
		case '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}

// ServiceError deliberately excludes remote messages and transport details:
// neither an untrusted peer nor a redirect may reflect a credential into logs.
type ServiceError struct {
	Code   string
	Status int
}

func (e *ServiceError) Error() string {
	switch e.Code {
	case "unsupported", "services_unsupported", "not_found":
		return "Service inventory is unsupported; upgrade Stardust or use a systemd Linux host"
	case "logs_unsupported":
		return "Service logs are unsupported; use a systemd Linux host with a supported Stardust version"
	case "logs_disabled":
		return "Service log access is disabled; configure Stardust's separate log token and unit allowlist"
	case "logs_unauthorized":
		return "Service log token was rejected; check service_log_token_file and the agent policy"
	case "log_unit_forbidden":
		return "Service is outside the agent's log-read allowlist"
	case "log_unit_not_found":
		return "Service unit no longer exists; refresh the inventory"
	case "logs_permission_denied":
		return "Stardust lacks permission to read the systemd journal"
	case "log_cursor_expired":
		return "Service log cursor expired; restart history or follow from recent logs"
	case "logs_timeout":
		return "Service log request timed out or was canceled"
	case "logs_unavailable":
		return "Systemd journal is unavailable; check the agent's journal access"
	case "invalid_log_query":
		return "Service log query or cursor was rejected"
	case "services_disabled":
		return "Service inventory is disabled; configure Stardust's private service policy and allowlist"
	case "unauthorized":
		return "Service read token was rejected; check service_token_file and the agent policy"
	case "unit_forbidden":
		return "Service is outside the agent's unit allowlist"
	case "unit_not_found":
		return "Service unit no longer exists; refresh the inventory"
	case "services_permission_denied":
		return "Stardust lacks permission to read systemd service metadata"
	case "services_timeout":
		return "Service request timed out; refresh to retry"
	case "services_unavailable":
		return "Systemd service metadata is unavailable; check the agent and its systemd access"
	case "invalid_query", "invalid_unit":
		return "Service query or unit was rejected; refresh the inventory"
	default:
		return fmt.Sprintf("Service request failed (HTTP %d)", e.Status)
	}
}

type ServicesClient struct {
	address, tokenFile string
	http               *http.Client
}

func NewServices(address, tokenFile string, timeout time.Duration) (*ServicesClient, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid service API address")
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("service credentials require HTTPS or a numeric-loopback HTTP address through a trusted SSH tunnel")
		}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &ServicesClient{address: strings.TrimRight(address, "/"), tokenFile: tokenFile, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *ServicesClient) Capabilities(ctx context.Context) (ServiceCapabilities, error) {
	var response struct {
		APIVersion string              `json:"api_version"`
		Services   ServiceCapabilities `json:"services"`
	}
	if err := c.get(ctx, "/api/v1/capabilities", "", &response); err != nil {
		return response.Services, err
	}
	if response.APIVersion != APIVersion {
		return ServiceCapabilities{}, errors.New("incompatible service API version; upgrade Orbit/Stardust")
	}
	return response.Services, nil
}

func (c *ServicesClient) token(ctx context.Context) (string, error) {
	capabilities, err := c.Capabilities(ctx)
	if err != nil {
		return "", err
	}
	if !capabilities.Supported {
		return "", &ServiceError{Code: "unsupported"}
	}
	if !capabilities.Enabled {
		return "", &ServiceError{Code: "services_disabled"}
	}
	if capabilities.Authentication != "bearer" {
		return "", errors.New("unsupported service authentication scheme; upgrade Orbit/Stardust")
	}
	return credentials.ServiceToken(c.tokenFile)
}

func (c *ServicesClient) List(ctx context.Context, query ServiceQuery) (ServicePage, error) {
	if query.Limit == 0 {
		query.Limit = 20
	}
	if query.Limit < 1 || query.Limit > 50 || len(query.Filter) > 255 || (query.Cursor != "" && !ValidServiceUnit(query.Cursor)) {
		return ServicePage{}, errors.New("invalid service query: limit must be 1–50, filter at most 255 bytes, and cursor a returned unit name")
	}
	token, err := c.token(ctx)
	if err != nil {
		return ServicePage{}, err
	}
	values := url.Values{"limit": {strconv.Itoa(query.Limit)}}
	if query.Filter != "" {
		values.Set("filter", query.Filter)
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	var response struct {
		APIVersion string      `json:"api_version"`
		Data       ServicePage `json:"data"`
	}
	if err := c.get(ctx, "/api/v1/services?"+values.Encode(), token, &response); err != nil {
		return ServicePage{}, err
	}
	if response.APIVersion != APIVersion {
		return ServicePage{}, errors.New("incompatible service API version; upgrade Orbit/Stardust")
	}
	if len(response.Data.Units) > query.Limit || (response.Data.NextCursor != "" && (!ValidServiceUnit(response.Data.NextCursor) || response.Data.NextCursor <= query.Cursor)) {
		return ServicePage{}, errors.New("invalid service pagination response")
	}
	last := query.Cursor
	for _, unit := range response.Data.Units {
		if !ValidServiceUnit(unit.Name) || unit.Name <= last {
			return ServicePage{}, errors.New("invalid service list identity or ordering")
		}
		last = unit.Name
	}
	if response.Data.NextCursor != "" && (len(response.Data.Units) == 0 || response.Data.NextCursor != last) {
		return ServicePage{}, errors.New("invalid service pagination cursor")
	}
	for index := range response.Data.Units {
		redactService(&response.Data.Units[index], token)
	}
	return response.Data, nil
}

func (c *ServicesClient) Detail(ctx context.Context, name string) (ServiceUnit, error) {
	if !ValidServiceUnit(name) {
		return ServiceUnit{}, errors.New("a valid .service unit name is required")
	}
	token, err := c.token(ctx)
	if err != nil {
		return ServiceUnit{}, err
	}
	var response struct {
		APIVersion string      `json:"api_version"`
		Data       ServiceUnit `json:"data"`
	}
	if err := c.get(ctx, "/api/v1/services/"+url.PathEscape(name), token, &response); err != nil {
		return ServiceUnit{}, err
	}
	if response.APIVersion != APIVersion {
		return ServiceUnit{}, errors.New("incompatible service API version; upgrade Orbit/Stardust")
	}
	if response.Data.Name != name {
		return ServiceUnit{}, errors.New("service detail identity does not match request")
	}
	redactService(&response.Data, token)
	return response.Data, nil
}

func (c *ServicesClient) Logs(ctx context.Context, tokenFile, name string, query ServiceLogQuery) (ServiceLogPage, error) {
	if !ValidServiceUnit(name) {
		return ServiceLogPage{}, errors.New("a valid .service unit name is required")
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 1 || query.Limit > MaxServiceLogEntries || (query.Before != "" && query.After != "") || !validLogCursor(query.Before) || !validLogCursor(query.After) || (!query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until)) {
		return ServiceLogPage{}, errors.New("invalid service log query")
	}
	capabilities, err := c.Capabilities(ctx)
	if err != nil {
		var serviceErr *ServiceError
		if errors.As(err, &serviceErr) && (serviceErr.Code == "unsupported" || serviceErr.Code == "services_unsupported") {
			return ServiceLogPage{}, &ServiceError{Code: "logs_unsupported", Status: serviceErr.Status}
		}
		return ServiceLogPage{}, err
	}
	if !capabilities.Supported {
		return ServiceLogPage{}, &ServiceError{Code: "logs_unsupported"}
	}
	if !capabilities.Enabled {
		return ServiceLogPage{}, &ServiceError{Code: "logs_disabled"}
	}
	if capabilities.Authentication != "bearer" {
		return ServiceLogPage{}, errors.New("unsupported service log authentication scheme; upgrade Orbit/Stardust")
	}
	token, err := credentials.ServiceLogToken(tokenFile)
	if err != nil {
		return ServiceLogPage{}, err
	}
	values := url.Values{"limit": {strconv.Itoa(query.Limit)}}
	if query.Before != "" {
		values.Set("before", query.Before)
	}
	if query.After != "" {
		values.Set("after", query.After)
	}
	if !query.Since.IsZero() {
		values.Set("since", query.Since.UTC().Format(time.RFC3339Nano))
	}
	if !query.Until.IsZero() {
		values.Set("until", query.Until.UTC().Format(time.RFC3339Nano))
	}
	var response struct {
		APIVersion string         `json:"api_version"`
		Data       ServiceLogPage `json:"data"`
	}
	endpoint := "/api/v1/services/" + url.PathEscape(name) + "/logs?" + values.Encode()
	if err := c.getBounded(ctx, endpoint, token, &response, 2*1024*1024); err != nil {
		var serviceErr *ServiceError
		if errors.As(err, &serviceErr) && (serviceErr.Code == "services_unsupported" || serviceErr.Code == "not_found") {
			return ServiceLogPage{}, &ServiceError{Code: "logs_unsupported", Status: serviceErr.Status}
		}
		return ServiceLogPage{}, err
	}
	if response.APIVersion != APIVersion {
		return ServiceLogPage{}, errors.New("incompatible service log API version; upgrade Orbit/Stardust")
	}
	page := response.Data
	if !ValidServiceUnit(page.Unit) || len(page.Entries) > query.Limit || !validLogCursor(page.NextCursor) || !validLogCursor(page.LatestCursor) {
		return ServiceLogPage{}, errors.New("invalid service log response")
	}
	for _, entry := range page.Entries {
		if entry.Timestamp.IsZero() || (entry.Priority != nil && (*entry.Priority < 0 || *entry.Priority > 7)) || len(entry.Message) > MaxServiceLogMessageBytes || entry.Cursor == "" || !validLogCursor(entry.Cursor) {
			return ServiceLogPage{}, errors.New("invalid service log entry")
		}
	}
	if page.HasMore {
		if query.After != "" && page.LatestCursor == "" || query.After == "" && page.NextCursor == "" {
			return ServiceLogPage{}, errors.New("service log response omitted its continuation cursor")
		}
	}
	if len(page.Entries) > 0 {
		if page.LatestCursor == "" || page.LatestCursor != page.Entries[len(page.Entries)-1].Cursor {
			return ServiceLogPage{}, errors.New("service log response has an invalid latest cursor")
		}
		if page.NextCursor != "" && page.NextCursor != page.Entries[0].Cursor {
			return ServiceLogPage{}, errors.New("service log response has an invalid older cursor")
		}
	}
	redactServiceLogs(&page, token)
	return page, nil
}

func validLogCursor(cursor string) bool {
	if cursor == "" {
		return true
	}
	if len(cursor) > MaxServiceLogCursorBytes || strings.TrimSpace(cursor) != cursor {
		return false
	}
	for _, character := range cursor {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func redactServiceLogs(page *ServiceLogPage, token string) {
	page.Unit = strings.ReplaceAll(page.Unit, token, "[redacted]")
	for index := range page.Entries {
		page.Entries[index].Message = strings.ReplaceAll(page.Entries[index].Message, token, "[redacted]")
	}
}

func redactService(unit *ServiceUnit, token string) {
	for _, field := range []*string{&unit.Name, &unit.ID, &unit.Description, &unit.LoadState, &unit.ActiveState, &unit.SubState, &unit.UnitFileState} {
		*field = strings.ReplaceAll(*field, token, "[redacted]")
	}
	if unit.MainProcess != nil {
		unit.MainProcess.Name = strings.ReplaceAll(unit.MainProcess.Name, token, "[redacted]")
	}
}

func (c *ServicesClient) get(ctx context.Context, endpoint, token string, target any) error {
	return c.getBounded(ctx, endpoint, token, target, 4*1024*1024)
}

func (c *ServicesClient) getBounded(ctx context.Context, endpoint, token string, target any, maxResponse int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.address+endpoint, nil)
	if err != nil {
		return errors.New("cannot create service request")
	}
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("service connection failed or timed out; check the agent/tunnel and refresh")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || int64(len(data)) > maxResponse {
		return errors.New("service response could not be read or exceeded the size limit")
	}
	if response.StatusCode != http.StatusOK {
		var body ErrorResponse
		_ = json.Unmarshal(data, &body)
		// Whitelist codes so even a malicious error code cannot echo secrets.
		code := ""
		switch body.Error.Code {
		case "services_unsupported", "not_found", "services_disabled", "unauthorized", "unit_forbidden", "unit_not_found", "services_permission_denied", "services_timeout", "services_unavailable", "invalid_query", "invalid_unit", "logs_disabled", "logs_unauthorized", "logs_unsupported", "log_unit_forbidden", "log_unit_not_found", "log_cursor_expired", "logs_permission_denied", "logs_timeout", "logs_unavailable", "invalid_log_query":
			code = body.Error.Code
		}
		if response.StatusCode == 404 && endpoint == "/api/v1/capabilities" {
			code = "unsupported"
		}
		return &ServiceError{Code: code, Status: response.StatusCode}
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("invalid service API response")
	}
	return nil
}
