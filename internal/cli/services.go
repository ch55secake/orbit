package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
	"github.com/ch55secake/orbit/internal/sshclient"
	"github.com/ch55secake/orbit/internal/tunnel"
	"github.com/spf13/cobra"
)

func servicesCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{Use: "services", Short: "Browse authorized systemd services through Stardust (read-only)"}
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
	command.AddCommand(list, detail, logs)
	return command
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
