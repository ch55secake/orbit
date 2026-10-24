package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/sshclient"
	"github.com/ch55secake/orbit/internal/tunnel"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

func servicesCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{Use: "services", Short: "Browse authorized systemd services through Stardust"}
	var query client.ServiceQuery
	list := &cobra.Command{Use: "list <server>", Short: "List one page of authorized services", Args: cobra.ExactArgs(1)}
	list.Flags().IntVar(&query.Limit, "limit", 20, "page size (1–50)")
	list.Flags().StringVar(&query.Filter, "filter", "", "case-sensitive unit-name substring")
	list.Flags().StringVar(&query.Cursor, "cursor", "", "next_cursor returned by the previous page")
	list.RunE = func(cmd *cobra.Command, args []string) error {
		return runServices(cmd, *configPath, args[0], query, "")
	}
	detail := &cobra.Command{Use: "show <server> <unit.service>", Short: "Inspect the current state and main process of a service", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return runServices(cmd, *configPath, args[0], client.ServiceQuery{}, args[1])
	}}
	var logLimit int
	var sinceText, untilText, before, after string
	var follow bool
	logs := &cobra.Command{
		Use:   "logs <server> <unit.service>",
		Short: "Read bounded service logs, optionally following new entries",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := client.ServiceLogQuery{Limit: logLimit, Before: before, After: after}
			var err error
			if sinceText != "" {
				query.Since, err = time.Parse(time.RFC3339Nano, sinceText)
				if err != nil {
					return errors.New("--since must be an RFC3339 timestamp")
				}
			}
			if untilText != "" {
				query.Until, err = time.Parse(time.RFC3339Nano, untilText)
				if err != nil {
					return errors.New("--until must be an RFC3339 timestamp")
				}
			}
			return runServiceLogs(cmd, *configPath, args[0], args[1], query, follow)
		},
	}
	logs.Flags().IntVar(&logLimit, "limit", 50, "number of entries (1–100)")
	logs.Flags().StringVar(&sinceText, "since", "", "start time (RFC3339)")
	logs.Flags().StringVar(&untilText, "until", "", "end time (RFC3339)")
	logs.Flags().StringVar(&before, "before", "", "opaque cursor for older history")
	logs.Flags().StringVar(&after, "after", "", "opaque cursor for follow/reconnect")
	logs.Flags().BoolVar(&follow, "follow", false, "poll for new entries until canceled")
	var yes bool
	restart := &cobra.Command{
		Use:   "restart <server> <unit.service>",
		Short: "Restart an explicitly authorized systemd service",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceRestart(cmd, *configPath, args[0], args[1], yes)
		},
	}
	restart.Flags().BoolVar(&yes, "yes", false, "confirm restart without an interactive prompt")
	operationStatus := &cobra.Command{
		Use:   "operation <server> <operation_id>",
		Short: "Inspect a systemd restart operation without issuing a mutation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceRestartStatus(cmd, *configPath, args[0], args[1])
		},
	}
	command.AddCommand(list, detail, logs, restart, operationStatus)
	return command
}

func runServiceRestartStatus(cmd *cobra.Command, path, name, operationID string) (runErr error) {
	if !client.ValidServiceRestartOperationID(operationID) {
		return errors.New("a valid restart operation identifier is required")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	target, err := sshclient.Find(cfg, name)
	if err != nil {
		return err
	}
	tunnels := tunnel.NewManager()
	defer func() { runErr = errors.Join(runErr, tunnels.Close()) }()
	operation, err := server.ReadServiceRestartStatus(cmd.Context(), target, tunnels, promptSSHPassword, operationID)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Restart operation %s for %s: %s", operation.ID, client.ServiceText(operation.Unit), client.ServiceText(operation.State)); err != nil {
		return err
	}
	if operation.JobResult != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), " (%s)", client.ServiceText(operation.JobResult)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
		return err
	}
	if operation.ErrorCode != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Operation detail: %s\n", client.ServiceRestartErrorText(operation.ErrorCode)); err != nil {
			return err
		}
	}
	if operation.ObservationError != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Systemd state observation: %s\n", client.ServiceText(operation.ObservationError)); err != nil {
			return err
		}
	}
	if operation.State == client.RestartSucceeded {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Systemd reports the job succeeded; this is not an application health check.")
		return err
	}
	if operation.State == client.RestartTimedOut || operation.State == client.RestartCancelled || operation.State == client.RestartInterrupted {
		return errors.New("operation outcome is unknown; inspect the service before attempting another restart")
	}
	if operation.State == client.RestartFailed {
		return errors.New("systemd reported that the restart job failed")
	}
	return nil
}

func runServiceRestart(cmd *cobra.Command, path, name, unit string, yes bool) (runErr error) {
	if !client.ValidServiceUnit(unit) {
		return errors.New("a valid .service unit name is required")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	target, err := sshclient.Find(cfg, name)
	if err != nil {
		return err
	}
	tunnels := tunnel.NewManager()
	defer func() { runErr = errors.Join(runErr, tunnels.Close()) }()
	if _, _, err := server.ReadServiceRestartAccess(cmd.Context(), target, tunnels, promptSSHPassword, unit); err != nil {
		return err
	}
	if err := confirmServiceRestart(cmd.InOrStdin(), cmd.ErrOrStderr(), target.Name, unit, yes, term.IsTerminal(os.Stdin.Fd())); err != nil {
		return err
	}
	key, err := client.NewServiceRestartKey()
	if err != nil {
		return err
	}
	operation, err := server.StartServiceRestart(cmd.Context(), target, tunnels, promptSSHPassword, unit, key)
	if err != nil {
		var serviceErr *client.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.Code == "restart_transport" {
			_, detail, refreshErr := server.ReadServices(cmd.Context(), target, tunnels, promptSSHPassword, client.ServiceQuery{}, unit)
			if refreshErr == nil && detail != nil {
				if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), "Latest systemd state after an ambiguous restart request (not application health):"); writeErr != nil {
					return writeErr
				}
				if writeErr := printServiceDetail(cmd.ErrOrStderr(), *detail); writeErr != nil {
					return writeErr
				}
			}
		}
		return err
	}
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Restart operation %s accepted for %s on %s.\n", operation.ID, client.ServiceText(unit), client.ServiceText(target.Name)); err != nil {
		return err
	}
	operation, err = waitServiceRestart(cmd.Context(), operation, func(ctx context.Context, id string) (client.ServiceRestartOperation, error) {
		return server.ReadServiceRestartStatus(ctx, target, tunnels, promptSSHPassword, id)
	}, 500*time.Millisecond, 40*time.Second)
	if err != nil {
		if operation.ID != "" {
			_, detail, refreshErr := server.ReadServices(cmd.Context(), target, tunnels, promptSSHPassword, client.ServiceQuery{}, unit)
			if refreshErr == nil && detail != nil {
				if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), "Latest systemd state (not an application health check):"); writeErr != nil {
					return writeErr
				}
				if writeErr := printServiceDetail(cmd.ErrOrStderr(), *detail); writeErr != nil {
					return writeErr
				}
			}
		}
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Restart operation %s: %s", operation.ID, operation.State); err != nil {
		return err
	}
	if operation.JobResult != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), " (%s)", client.ServiceText(operation.JobResult)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
		return err
	}
	if operation.ErrorCode != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Operation detail: %s\n", client.ServiceRestartErrorText(operation.ErrorCode)); err != nil {
			return err
		}
	}
	if operation.ObservationError != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Systemd state observation: %s\n", client.ServiceText(operation.ObservationError)); err != nil {
			return err
		}
	}
	_, detail, refreshErr := server.ReadServices(cmd.Context(), target, tunnels, promptSSHPassword, client.ServiceQuery{}, unit)
	if refreshErr != nil {
		if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "Service detail refresh failed: %s\n", client.ServiceText(refreshErr.Error())); writeErr != nil {
			return writeErr
		}
	} else if detail != nil {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Current systemd state (not application health):"); err != nil {
			return err
		}
		if err := printServiceDetail(cmd.OutOrStdout(), *detail); err != nil {
			return err
		}
	}
	switch operation.State {
	case client.RestartSucceeded:
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Systemd reported the restart job succeeded; this is not an application health check."); err != nil {
			return err
		}
	case client.RestartFailed:
		return errors.New("systemd reported that the restart job failed")
	case client.RestartTimedOut, client.RestartCancelled, client.RestartInterrupted:
		return fmt.Errorf("restart outcome is unknown (%s); inspect the operation and service before attempting another restart", operation.State)
	default:
		return fmt.Errorf("restart operation ended in unexpected state %q", client.ServiceText(operation.State))
	}
	return nil
}

func confirmServiceRestart(in io.Reader, out io.Writer, serverName, unit string, yes, interactive bool) error {
	if yes {
		return nil
	}
	if !interactive {
		return errors.New("service restart requires explicit confirmation; rerun interactively or pass --yes")
	}
	phrase := "restart " + serverName + " " + unit
	if _, err := fmt.Fprintf(out, "Restart %s on %s? This may interrupt the service. Type %q to confirm: ", client.ServiceText(unit), client.ServiceText(serverName), phrase); err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(in, 256)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.New("could not read restart confirmation")
	}
	if strings.TrimSpace(line) != phrase {
		return errors.New("service restart canceled; no restart request was sent")
	}
	return nil
}

type serviceRestartStatusFunc func(context.Context, string) (client.ServiceRestartOperation, error)

func waitServiceRestart(ctx context.Context, operation client.ServiceRestartOperation, read serviceRestartStatusFunc, interval, timeout time.Duration) (client.ServiceRestartOperation, error) {
	if operation.State != client.RestartRunning {
		return operation, nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return operation, fmt.Errorf("restart operation %s remains unresolved; outcome is unknown, inspect the service before retrying: %w", operation.ID, ctx.Err())
		case <-deadline.C:
			if lastErr != nil {
				return operation, fmt.Errorf("restart operation %s remains unresolved; outcome is unknown, inspect the service before retrying: %w", operation.ID, lastErr)
			}
			return operation, fmt.Errorf("restart operation %s is still running; outcome is unknown, inspect the operation before retrying", operation.ID)
		case <-ticker.C:
			updated, err := read(ctx, operation.ID)
			if err != nil {
				lastErr = err
				if !client.RetryableRestartStatusError(err) {
					return operation, fmt.Errorf("restart operation %s could not be reconciled; outcome is unknown, inspect the service before retrying: %w", operation.ID, err)
				}
				continue
			}
			if updated.ID != operation.ID || updated.Unit != operation.Unit {
				return operation, fmt.Errorf("restart operation %s returned a mismatched unit identity; outcome remains unknown", operation.ID)
			}
			lastErr = nil
			operation = updated
			if operation.State != client.RestartRunning {
				return operation, nil
			}
		}
	}
}

func runServiceLogs(cmd *cobra.Command, path, name, unit string, query client.ServiceLogQuery, follow bool) (runErr error) {
	if !client.ValidServiceUnit(unit) {
		return errors.New("a valid .service unit name is required")
	}
	if query.Limit < 1 || query.Limit > client.MaxServiceLogEntries {
		return errors.New("--limit must be between 1 and 100")
	}
	if query.Before != "" && query.After != "" {
		return errors.New("--before and --after cannot be combined")
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until) {
		return errors.New("--since must not be later than --until")
	}
	if follow && query.Before != "" {
		return errors.New("--follow cannot be combined with --before")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	target, err := sshclient.Find(cfg, name)
	if err != nil {
		return err
	}
	tunnels := tunnel.NewManager()
	defer func() { runErr = errors.Join(runErr, tunnels.Close()) }()
	reader := func(ctx context.Context, query client.ServiceLogQuery) (client.ServiceLogPage, error) {
		return server.ReadServiceLogs(ctx, target, tunnels, promptSSHPassword, unit, query)
	}
	return streamServiceLogs(cmd.Context(), reader, unit, query, follow, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

type serviceLogReadFunc func(context.Context, client.ServiceLogQuery) (client.ServiceLogPage, error)

func streamServiceLogs(ctx context.Context, read serviceLogReadFunc, unit string, query client.ServiceLogQuery, follow bool, out, errOut io.Writer) error {
	page, err := read(ctx, query)
	if err != nil {
		return err
	}
	if len(page.Entries) == 0 && !follow {
		if _, err := fmt.Fprintln(out, "No log entries in this time range."); err != nil {
			return err
		}
	} else if err := writeServiceLogs(out, unit, page.Entries); err != nil {
		return err
	}
	if page.NextCursor != "" {
		if _, err := fmt.Fprintf(errOut, "Older page available with --before %q (keep the same time filters).\n", page.NextCursor); err != nil {
			return err
		}
	}
	if !follow {
		cursor := page.LatestCursor
		if cursor == "" {
			cursor = query.After
		}
		if cursor != "" && query.Before == "" {
			_, err = fmt.Fprintf(errOut, "Resume following with --after %q (keep the same time filters).\n", cursor)
		}
		return err
	}
	if _, err := fmt.Fprintf(errOut, "Following %s logs; press Ctrl+C to stop.\n", client.ServiceText(unit)); err != nil {
		return err
	}
	if len(page.Entries) == 0 {
		if _, err := fmt.Fprintln(errOut, "No current log entries; waiting for new entries."); err != nil {
			return err
		}
	}
	baseQuery := query
	baseQuery.Before = ""
	cursor := query.After
	if page.LatestCursor != "" {
		cursor = page.LatestCursor
	}
	delay := time.Second
	for {
		if page.HasMore && cursor != "" {
			baseQuery.After = cursor
			page, err = read(ctx, baseQuery)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if fatalServiceLogError(err) {
					return err
				}
				if _, writeErr := fmt.Fprintf(errOut, "Log poll failed; retrying: %s\n", client.ServiceText(err.Error())); writeErr != nil {
					return writeErr
				}
				delay = min(delay*2, 30*time.Second)
				page = client.ServiceLogPage{}
				continue
			}
			if err := writeServiceLogs(out, unit, page.Entries); err != nil {
				return err
			}
			if page.LatestCursor != "" {
				cursor = page.LatestCursor
			}
			delay = time.Second
			continue
		}
		baseQuery.After = cursor
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
		hadCursor := cursor != ""
		page, err = read(ctx, baseQuery)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fatalServiceLogError(err) {
				return err
			}
			if _, writeErr := fmt.Fprintf(errOut, "Log poll failed; retrying: %s\n", client.ServiceText(err.Error())); writeErr != nil {
				return writeErr
			}
			delay = min(delay*2, 30*time.Second)
			page = client.ServiceLogPage{}
			continue
		}
		if err := writeServiceLogs(out, unit, page.Entries); err != nil {
			return err
		}
		if !hadCursor && page.HasMore {
			if _, err := fmt.Fprintln(errOut, "More history exists than this bounded page; some entries may have been omitted before the follow cursor was established."); err != nil {
				return err
			}
		}
		if page.LatestCursor != "" {
			cursor = page.LatestCursor
		}
		delay = time.Second
	}
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

func writeServiceLogs(out io.Writer, unit string, entries []client.ServiceLogEntry) error {
	for _, entry := range entries {
		priority := "-"
		if entry.Priority != nil {
			priority = serviceLogPriority(*entry.Priority)
		}
		message := client.ServiceLogText(entry.Message)
		lines := strings.Split(message, "\n")
		prefix := fmt.Sprintf("%s %-7s %s: ", entry.Timestamp.UTC().Format(time.RFC3339Nano), priority, client.ServiceText(unit))
		if _, err := fmt.Fprintln(out, prefix+lines[0]); err != nil {
			return err
		}
		for _, line := range lines[1:] {
			if _, err := fmt.Fprintf(out, "%s  | %s\n", strings.Repeat(" ", len(prefix)), line); err != nil {
				return err
			}
		}
		if entry.MessageTruncated {
			if _, err := fmt.Fprintf(out, "%s  … message truncated by Stardust\n", strings.Repeat(" ", len(prefix))); err != nil {
				return err
			}
		}
	}
	return nil
}

func serviceLogPriority(priority int) string {
	labels := [...]string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}
	if priority < 0 || priority >= len(labels) {
		return "-"
	}
	return labels[priority]
}

func runServices(cmd *cobra.Command, path, name string, query client.ServiceQuery, unit string) (runErr error) {
	if unit == "" && (query.Limit < 1 || query.Limit > 50) {
		return errors.New("--limit must be between 1 and 50")
	}
	if unit != "" && !client.ValidServiceUnit(unit) {
		return errors.New("a valid .service unit name is required")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	target, err := sshclient.Find(cfg, name)
	if err != nil {
		return err
	}
	tunnels := tunnel.NewManager()
	defer func() { runErr = errors.Join(runErr, tunnels.Close()) }()
	page, detail, err := server.ReadServices(cmd.Context(), target, tunnels, promptSSHPassword, query, unit)
	if err != nil {
		return err
	}
	if detail != nil {
		return printServiceDetail(cmd.OutOrStdout(), *detail)
	}
	if len(page.Units) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No authorized services match this filter.")
		return err
	}
	for _, entry := range page.Units {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s/%s  PID %d  %s\n", client.ServiceText(entry.Name), client.ServiceText(entry.ActiveState), client.ServiceText(entry.SubState), entry.MainPID, client.ServiceText(entry.Description)); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Next page: --cursor %q (keep the same --filter)\n", page.NextCursor)
		return err
	}
	return nil
}

func printServiceDetail(out io.Writer, unit client.ServiceUnit) error {
	pid := "none"
	if unit.MainPID != 0 {
		pid = fmt.Sprint(unit.MainPID)
	}
	_, err := fmt.Fprintf(out, "Service: %s\nCanonical ID: %s\nDescription: %s\nState: %s / %s\nLoad: %s\nUnit file: %s\nMain PID: %s\n", client.ServiceText(unit.Name), client.ServiceText(unit.ID), client.ServiceText(unit.Description), client.ServiceText(unit.ActiveState), client.ServiceText(unit.SubState), client.ServiceText(unit.LoadState), client.ServiceText(unit.UnitFileState), pid)
	if err != nil {
		return err
	}
	if process := unit.MainProcess; process != nil {
		_, err = fmt.Fprintf(out, "Process: %s (effective UID %d)\n", client.ServiceText(process.Name), process.EffectiveUID)
	}
	return err
}
