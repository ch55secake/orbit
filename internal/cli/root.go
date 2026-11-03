package cli

import (
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
	"github.com/ch55secake/orbit/internal/tui"
	"github.com/ch55secake/orbit/internal/tunnel"
	"github.com/charmbracelet/x/term"
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
		sshCommand(&configPath),
		stardustCommand(&configPath),
		servicesCommand(&configPath),
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
					connection := ""
					if server.SSH != nil {
						connection = " [SSH via " + server.SSH.Host + "]"
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s%s%s\n", server.Name, server.Address, connection, description); err != nil {
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
	var name, address, description, serviceTokenFile, serviceLogTokenFile, serviceRestartTokenFile string
	var ssh sshFlags
	command := &cobra.Command{
		Use:   "add",
		Short: "Register a server in local configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" || strings.TrimSpace(address) == "" {
				return fmt.Errorf("--name and --address are required")
			}
			sshConfig, err := ssh.configForAdd(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			if err := cfg.AddServer(config.Server{Name: name, Address: address, Description: description, SSH: sshConfig, ServiceTokenFile: serviceTokenFile, ServiceLogTokenFile: serviceLogTokenFile, ServiceRestartTokenFile: serviceRestartTokenFile}); err != nil {
				return err
			}
			return config.Save(*configPath, cfg)
		},
	}
	command.Flags().StringVar(&name, "name", "", "server name")
	command.Flags().StringVar(&address, "address", "", "Stardust base URL")
	command.Flags().StringVar(&description, "description", "", "optional description")
	command.Flags().StringVar(&serviceTokenFile, "service-token-file", "", "absolute path to private service read token file (not the token)")
	command.Flags().StringVar(&serviceLogTokenFile, "service-log-token-file", "", "absolute path to private service log token file (not the token)")
	command.Flags().StringVar(&serviceRestartTokenFile, "service-restart-token-file", "", "absolute path to private service restart token file (not the token)")
	ssh.addFlags(command)
	return command
}

func editServerCommand(configPath *string) *cobra.Command {
	var name, newName, address, description, serviceTokenFile, serviceLogTokenFile, serviceRestartTokenFile string
	var ssh sshFlags
	var direct bool
	command := &cobra.Command{
		Use:     "edit",
		Aliases: []string{"update"},
		Short:   "Update a configured server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if !cmd.Flags().Changed("new-name") && !cmd.Flags().Changed("address") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("service-token-file") && !cmd.Flags().Changed("service-log-token-file") && !cmd.Flags().Changed("service-restart-token-file") && !ssh.changed(cmd) && !cmd.Flags().Changed("direct") {
				return fmt.Errorf("at least one server setting must be provided")
			}
			if direct && ssh.changed(cmd) {
				return fmt.Errorf("--direct cannot be combined with SSH tunnel settings")
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
				if updated.Address != strings.TrimSpace(address) {
					updated.ServiceTokenFile = ""
					updated.ServiceLogTokenFile = ""
					updated.ServiceRestartTokenFile = ""
				}
				updated.Address = address
			}
			if cmd.Flags().Changed("service-token-file") {
				updated.ServiceTokenFile = serviceTokenFile
			}
			if cmd.Flags().Changed("service-log-token-file") {
				updated.ServiceLogTokenFile = serviceLogTokenFile
			}
			if cmd.Flags().Changed("service-restart-token-file") {
				updated.ServiceRestartTokenFile = serviceRestartTokenFile
			}
			if cmd.Flags().Changed("description") {
				updated.Description = description
			}
			if direct {
				updated.SSH = nil
			} else if ssh.changed(cmd) {
				if updated.SSH == nil {
					updated.SSH = &config.SSHConfig{RemoteHost: "127.0.0.1"}
				}
				ssh.apply(cmd, updated.SSH)
				if strings.TrimSpace(updated.SSH.Host) == "" {
					return fmt.Errorf("--ssh-host is required when configuring an SSH tunnel")
				}
				if updated.SSH.RemotePort == 0 {
					return fmt.Errorf("--ssh-remote-port is required when configuring an SSH tunnel")
				}
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
	command.Flags().StringVar(&serviceTokenFile, "service-token-file", "", "absolute path to private service read token file (empty clears it)")
	command.Flags().StringVar(&serviceLogTokenFile, "service-log-token-file", "", "absolute path to private service log token file (empty clears it)")
	command.Flags().StringVar(&serviceRestartTokenFile, "service-restart-token-file", "", "absolute path to private service restart token file (empty clears it)")
	command.Flags().BoolVar(&direct, "direct", false, "remove SSH tunnel settings")
	ssh.addFlags(command)
	return command
}

type sshFlags struct {
	host         string
	user         string
	port         int
	identityFile string
	remoteHost   string
	remotePort   int
}

func (f *sshFlags) addFlags(command *cobra.Command) {
	command.Flags().StringVar(&f.host, "ssh-host", "", "SSH host or ssh_config alias")
	command.Flags().StringVar(&f.user, "ssh-user", "", "SSH username (defaults to ssh_config)")
	command.Flags().IntVar(&f.port, "ssh-port", 22, "SSH server port")
	command.Flags().StringVar(&f.identityFile, "ssh-identity-file", "", "SSH private key file")
	command.Flags().StringVar(&f.remoteHost, "ssh-remote-host", "127.0.0.1", "Stardust host as seen from the SSH server")
	command.Flags().IntVar(&f.remotePort, "ssh-remote-port", 0, "Stardust port as seen from the SSH server")
}

func (f sshFlags) changed(command *cobra.Command) bool {
	for _, name := range []string{"ssh-host", "ssh-user", "ssh-port", "ssh-identity-file", "ssh-remote-host", "ssh-remote-port"} {
		if command.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func (f sshFlags) configForAdd(command *cobra.Command) (*config.SSHConfig, error) {
	if !f.changed(command) {
		return nil, nil
	}
	if strings.TrimSpace(f.host) == "" {
		return nil, fmt.Errorf("--ssh-host is required when configuring an SSH tunnel")
	}
	if f.remotePort == 0 {
		return nil, fmt.Errorf("--ssh-remote-port is required when configuring an SSH tunnel")
	}
	return &config.SSHConfig{
		Host:         f.host,
		User:         f.user,
		Port:         f.port,
		IdentityFile: f.identityFile,
		RemoteHost:   f.remoteHost,
		RemotePort:   f.remotePort,
	}, nil
}

func (f sshFlags) apply(command *cobra.Command, target *config.SSHConfig) {
	if command.Flags().Changed("ssh-host") {
		target.Host = f.host
	}
	if command.Flags().Changed("ssh-user") {
		target.User = f.user
	}
	if command.Flags().Changed("ssh-port") {
		target.Port = f.port
	}
	if command.Flags().Changed("ssh-identity-file") {
		target.IdentityFile = f.identityFile
	}
	if command.Flags().Changed("ssh-remote-host") {
		target.RemoteHost = f.remoteHost
	}
	if command.Flags().Changed("ssh-remote-port") {
		target.RemotePort = f.remotePort
	}
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
			tunnels := tunnel.NewManager()
			statusErr := printStatus(cmd.Context(), cmd.OutOrStdout(), cfg, client.New(timeout), tunnels, promptSSHPassword)
			return errors.Join(statusErr, tunnels.Close())
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "HTTP request timeout")
	return command
}

func printStatus(ctx context.Context, out io.Writer, cfg config.Config, apiClient server.MetricsClient, tunnels server.TunnelManager, passwordPrompt server.PasswordPrompt) error {
	if len(cfg.Servers) == 0 {
		_, err := fmt.Fprintln(out, "No servers configured.")
		return err
	}
	for index, target := range cfg.Servers {
		if index > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "%s (%s)\n", target.Name, target.Address); err != nil {
			return err
		}
		status := server.FetchMetricsWithPrompt(ctx, target, apiClient, tunnels, passwordPrompt)
		if target.SSH != nil {
			if _, err := fmt.Fprintf(out, "  tunnel: %s\n", status.TunnelState); err != nil {
				return err
			}
		}
		if status.Error != nil {
			if _, writeErr := fmt.Fprintf(out, "  status: offline (%v)\n", status.Error); writeErr != nil {
				return writeErr
			}
			continue
		}
		data := status.Metrics.Data
		if _, err := fmt.Fprintln(out, "  status: online"); err != nil {
			return err
		}
		if err := printMetrics(out, data); err != nil {
			return err
		}
	}
	return nil
}

func promptSSHPassword(ctx context.Context, target config.Server) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return nil, errors.New("SSH password authentication requires an interactive terminal; configure SSH key or agent authentication for non-interactive use")
	}
	user := target.SSH.User
	if user == "" {
		user = "SSH user"
	}
	if _, err := fmt.Fprintf(os.Stderr, "SSH password for %s@%s: ", user, target.SSH.Host); err != nil {
		return nil, err
	}
	password, err := term.ReadPassword(os.Stdin.Fd())
	_, writeErr := fmt.Fprintln(os.Stderr)
	if err != nil {
		clear(password)
		return nil, err
	}
	if writeErr != nil {
		clear(password)
		return nil, writeErr
	}
	return password, nil
}
