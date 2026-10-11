package cli

import (
	"fmt"

	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/sshclient"
	"github.com/spf13/cobra"
)

func sshCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use: "ssh <server>", Short: "Open an interactive SSH shell on a configured server", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			target, err := sshclient.Find(cfg, args[0])
			if err != nil {
				return err
			}
			process, err := sshclient.Command(cmd.Context(), target, true, "")
			if err != nil {
				return err
			}
			process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := process.Run(); err != nil {
				return fmt.Errorf("SSH session for %q: %w", target.Name, err)
			}
			return nil
		},
	}
}
