package cli

import (
	"errors"
	"fmt"
	"io"

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
	command.AddCommand(list, detail)
	return command
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
