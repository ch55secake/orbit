package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/tui"
	"github.com/spf13/cobra"
)

func New(version string) *cobra.Command {
	configPath := config.DefaultPath()

	root := &cobra.Command{
		Use:           "orbit",
		Short:         "Terminal-native mission control for your servers",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return tui.Run(configPath, version)
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", configPath, "configuration file path")

	root.AddCommand(
		versionCommand(version),
		serversCommand(&configPath),
		statusCommand(&configPath),
	)
	return root
}

func versionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Orbit version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "orbit "+version)
			return err
		},
	}
}

func serversCommand(configPath *string) *cobra.Command {
	servers := &cobra.Command{Use: "servers", Short: "Manage configured servers"}
	servers.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List configured servers",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := config.Load(*configPath)
				if err != nil {
					return err
				}
				if len(cfg.Servers) == 0 {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), "No servers configured.")
					return err
				}
				for _, server := range cfg.Servers {
					description := strings.TrimSpace(server.Description)
					if description != "" {
						description = " — " + description
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s%s\n", server.Name, server.Address, description); err != nil {
						return err
					}
				}
				return nil
			},
		},
		addServerCommand(configPath),
		editServerCommand(configPath),
		removeServerCommand(configPath),
	)
	return servers
}

func addServerCommand(configPath *string) *cobra.Command {
	var name, address, description string
	command := &cobra.Command{
		Use:   "add",
		Short: "Register a server in local configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" || strings.TrimSpace(address) == "" {
				return fmt.Errorf("--name and --address are required")
			}
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			if err := cfg.AddServer(config.Server{Name: name, Address: address, Description: description}); err != nil {
				return err
			}
			return config.Save(*configPath, cfg)
		},
	}
	command.Flags().StringVar(&name, "name", "", "server name")
	command.Flags().StringVar(&address, "address", "", "Stardust base URL")
	command.Flags().StringVar(&description, "description", "", "optional description")
	return command
}

func editServerCommand(configPath *string) *cobra.Command {
	var name, newName, address, description string
	command := &cobra.Command{
		Use:     "edit",
		Aliases: []string{"update"},
		Short:   "Update a configured server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if !cmd.Flags().Changed("new-name") && !cmd.Flags().Changed("address") && !cmd.Flags().Changed("description") {
				return fmt.Errorf("at least one of --new-name, --address, or --description is required")
			}

			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			var updated config.Server
			found := false
			for _, server := range cfg.Servers {
				if strings.TrimSpace(server.Name) == strings.TrimSpace(name) {
					updated = server
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("server %q not found", strings.TrimSpace(name))
			}
			if cmd.Flags().Changed("new-name") {
				updated.Name = newName
			}
			if cmd.Flags().Changed("address") {
				updated.Address = address
			}
			if cmd.Flags().Changed("description") {
				updated.Description = description
			}
			if err := cfg.UpdateServer(name, updated); err != nil {
				return err
			}
			return config.Save(*configPath, cfg)
		},
	}
	command.Flags().StringVar(&name, "name", "", "existing server name")
	command.Flags().StringVar(&newName, "new-name", "", "new server name")
	command.Flags().StringVar(&address, "address", "", "new Stardust base URL")
	command.Flags().StringVar(&description, "description", "", "new description (empty clears it)")
	return command
}

func removeServerCommand(configPath *string) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "remove",
		Short: "Remove a configured server",
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			if err := cfg.RemoveServer(name); err != nil {
				return err
			}
			return config.Save(*configPath, cfg)
		},
	}
	command.Flags().StringVar(&name, "name", "", "server name")
	return command
}

func statusCommand(configPath *string) *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "status",
		Short: "Fetch current metrics from configured servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			return printStatus(cmd.Context(), cmd.OutOrStdout(), cfg, client.New(timeout))
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "HTTP request timeout")
	return command
}

func printStatus(ctx context.Context, out io.Writer, cfg config.Config, apiClient *client.Client) error {
	if len(cfg.Servers) == 0 {
		_, err := fmt.Fprintln(out, "No servers configured.")
		return err
	}
	for index, server := range cfg.Servers {
		if index > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "%s (%s)\n", server.Name, server.Address); err != nil {
			return err
		}
		response, err := apiClient.Metrics(ctx, server.Address)
		if err != nil {
			if _, writeErr := fmt.Fprintf(out, "  status: offline (%v)\n", err); writeErr != nil {
				return writeErr
			}
			continue
		}
		data := response.Data
		if _, err := fmt.Fprintln(out, "  status: online"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  CPU: %.1f%% (%d cores)\n", data.CPU.UsagePercent, data.CPU.LogicalCores); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  Memory: %.1f%% (%s / %s)\n", data.Memory.UsedPercent, client.FormatBytes(data.Memory.UsedBytes), client.FormatBytes(data.Memory.TotalBytes)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  Disk: %.1f%% (%s / %s)\n", data.Disk.UsedPercent, client.FormatBytes(data.Disk.UsedBytes), client.FormatBytes(data.Disk.TotalBytes)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  Uptime: %s\n", client.FormatUptime(data.Uptime)); err != nil {
			return err
		}
		if len(data.Errors) > 0 {
			if _, err := fmt.Fprintf(out, "  unavailable: %d metric(s)\n", len(data.Errors)); err != nil {
				return err
			}
		}
	}
	return nil
}
