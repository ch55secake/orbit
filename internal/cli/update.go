package cli

import (
	"fmt"

	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/sshclient"
	"github.com/ch55secake/orbit/internal/update"
	"github.com/spf13/cobra"
)

func stardustCommand(configPath *string) *cobra.Command {
	parent := &cobra.Command{Use: "stardust", Short: "Manage remote Stardust agents"}
	var version string
	var yes bool
	command := &cobra.Command{
		Use: "update <server>", Short: "Update an existing remote Stardust service from a verified release", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			target, err := sshclient.Find(cfg, args[0])
			if err != nil {
				return err
			}
			process, err := update.Command(cmd.Context(), target, version)
			if err != nil {
				return err
			}
			if !yes {
				return fmt.Errorf("updating %q replaces its Stardust binary and restarts its service; pass --yes to confirm", target.Name)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Updating Stardust on %s (%s); existing service settings will be preserved.\n", target.Name, version); err != nil {
				return err
			}
			process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := process.Run(); err != nil {
				return fmt.Errorf("stardust update on %q: %w", target.Name, err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&version, "version", "latest", "release tag vX.Y.Z or latest")
	command.Flags().BoolVar(&yes, "yes", false, "confirm binary replacement and service restart")
	parent.AddCommand(command)
	return parent
}
