package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/viper"
)

const currentVersion = 1

type Config struct {
	Version int      `mapstructure:"version" yaml:"version"`
	Servers []Server `mapstructure:"servers" yaml:"servers"`
}

type Server struct {
	Name        string     `mapstructure:"name" yaml:"name"`
	Address     string     `mapstructure:"address" yaml:"address"`
	Description string     `mapstructure:"description" yaml:"description,omitempty"`
	SSH         *SSHConfig `mapstructure:"ssh" yaml:"ssh,omitempty"`
}

type SSHConfig struct {
	Host         string `mapstructure:"host" yaml:"host"`
	User         string `mapstructure:"user" yaml:"user,omitempty"`
	Port         int    `mapstructure:"port" yaml:"port,omitempty"`
	IdentityFile string `mapstructure:"identity_file" yaml:"identity_file,omitempty"`
	RemoteHost   string `mapstructure:"remote_host" yaml:"remote_host,omitempty"`
	RemotePort   int    `mapstructure:"remote_port" yaml:"remote_port"`
}

func DefaultPath() string {
	if override := os.Getenv("ORBIT_CONFIG"); override != "" {
		return override
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".config", "orbit", "config.yaml")
	}
	return filepath.Join(base, "orbit", "config.yaml")
}

func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	settings := viper.New()
	settings.SetConfigFile(path)
	settings.SetConfigType("yaml")
	if err := settings.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{Version: currentVersion}, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{Version: currentVersion}
	if err := settings.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = currentVersion
	}
	if cfg.Version != currentVersion {
		return Config{}, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	cfg.Version = currentVersion
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	settings := viper.New()
	settings.SetConfigFile(path)
	settings.SetConfigType("yaml")
	settings.SetConfigPermissions(0o600)
	settings.Set("version", cfg.Version)
	settings.Set("servers", cfg.Servers)
	if err := settings.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	seenNames := make(map[string]struct{}, len(c.Servers))
	seenTunnelPorts := make(map[int]int, len(c.Servers))
	for index, server := range c.Servers {
		if err := server.Validate(); err != nil {
			return fmt.Errorf("server %d: %w", index+1, err)
		}

		name := strings.TrimSpace(server.Name)
		if _, exists := seenNames[name]; exists {
			return fmt.Errorf("server %q is duplicated", name)
		}
		seenNames[name] = struct{}{}

		if server.SSH != nil {
			address, _ := url.Parse(strings.TrimSpace(server.Address))
			port, _ := strconv.Atoi(address.Port())
			if previous, exists := seenTunnelPorts[port]; exists {
				return fmt.Errorf("server %q shares local SSH tunnel port %d with server %q", name, port, c.Servers[previous].Name)
			}
			seenTunnelPorts[port] = index
		}
	}
	return nil
}

func (s Server) Validate() error {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return errors.New("server name must not be empty")
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return fmt.Errorf("server name %q contains a control character", s.Name)
		}
	}

	address := strings.TrimSpace(s.Address)
	if address == "" {
		return errors.New("server address must not be empty")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("server address %q is not a valid URL: %w", s.Address, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("server address %q must use http:// or https://", s.Address)
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("server address %q must include a host", s.Address)
	}
	if parsed.User != nil {
		return fmt.Errorf("server address %q must not include credentials", s.Address)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("server address %q must not include a query or fragment", s.Address)
	}
	if parsed.Opaque != "" || strings.HasSuffix(parsed.Host, ":") {
		return fmt.Errorf("server address %q contains an unsupported URL", s.Address)
	}
	if s.SSH != nil {
		if err := s.SSH.validate(parsed); err != nil {
			return err
		}
	}
	return nil
}

func (s SSHConfig) validate(address *url.URL) error {
	host := strings.TrimSpace(s.Host)
	if host == "" {
		return errors.New("SSH host must not be empty")
	}
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("SSH host %q is invalid", s.Host)
	}
	for _, character := range host {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && !strings.ContainsRune("._:-", character) {
			return fmt.Errorf("SSH host %q is invalid", s.Host)
		}
	}
	user := strings.TrimSpace(s.User)
	if user != "" {
		for _, character := range user {
			if !unicode.IsLetter(character) && !unicode.IsDigit(character) && !strings.ContainsRune("._-", character) {
				return fmt.Errorf("SSH user %q is invalid", s.User)
			}
		}
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("SSH port %d must be 0 (default) or between 1 and 65535", s.Port)
	}
	if s.RemotePort < 1 || s.RemotePort > 65535 {
		return fmt.Errorf("SSH remote port %d must be between 1 and 65535", s.RemotePort)
	}
	if s.IdentityFile != "" && hasControl(s.IdentityFile) {
		return errors.New("SSH identity file must not contain control characters")
	}
	remoteHost := strings.TrimSpace(s.RemoteHost)
	if remoteHost == "" {
		remoteHost = "127.0.0.1"
	}
	if remoteHost != "localhost" {
		ip := net.ParseIP(remoteHost)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("SSH remote host %q must be localhost or a loopback IP", s.RemoteHost)
		}
	}
	localHost := net.ParseIP(address.Hostname())
	if localHost == nil || !localHost.IsLoopback() {
		return fmt.Errorf("SSH tunnel address host %q must be a loopback IP", address.Hostname())
	}
	if address.Port() == "" {
		return errors.New("SSH tunnel address must include a local port")
	}
	localPort, err := strconv.Atoi(address.Port())
	if err != nil || localPort < 1 || localPort > 65535 {
		return fmt.Errorf("SSH tunnel address port %q must be between 1 and 65535", address.Port())
	}
	if address.Path != "" && address.Path != "/" {
		return errors.New("SSH tunnel address must not include a path")
	}
	return nil
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func (c *Config) AddServer(server Server) error {
	server = normalizeServer(server)
	if err := server.Validate(); err != nil {
		return err
	}
	for _, existing := range c.Servers {
		if strings.TrimSpace(existing.Name) == server.Name {
			return fmt.Errorf("server %q already exists", server.Name)
		}
	}
	c.Servers = append(c.Servers, server)
	return nil
}

func (c *Config) UpdateServer(name string, server Server) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server name must not be empty")
	}

	index := -1
	for currentIndex, existing := range c.Servers {
		if strings.TrimSpace(existing.Name) == name {
			index = currentIndex
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("server %q not found", name)
	}

	server = normalizeServer(server)
	if err := server.Validate(); err != nil {
		return err
	}
	for currentIndex, existing := range c.Servers {
		if currentIndex != index && strings.TrimSpace(existing.Name) == server.Name {
			return fmt.Errorf("server %q already exists", server.Name)
		}
	}
	c.Servers[index] = server
	return nil
}

func (c *Config) RemoveServer(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server name must not be empty")
	}

	for index, server := range c.Servers {
		if strings.TrimSpace(server.Name) == name {
			copy(c.Servers[index:], c.Servers[index+1:])
			c.Servers[len(c.Servers)-1] = Server{}
			c.Servers = c.Servers[:len(c.Servers)-1]
			return nil
		}
	}
	return fmt.Errorf("server %q not found", name)
}

func normalizeServer(server Server) Server {
	server.Name = strings.TrimSpace(server.Name)
	server.Address = strings.TrimSpace(server.Address)
	server.Description = strings.TrimSpace(server.Description)
	if server.SSH != nil {
		ssh := *server.SSH
		ssh.Host = strings.TrimSpace(ssh.Host)
		ssh.User = strings.TrimSpace(ssh.User)
		ssh.IdentityFile = strings.TrimSpace(ssh.IdentityFile)
		ssh.RemoteHost = strings.TrimSpace(ssh.RemoteHost)
		server.SSH = &ssh
	}
	return server
}
