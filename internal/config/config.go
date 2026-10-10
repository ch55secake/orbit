package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const currentVersion = 1

type Config struct {
	Version int      `yaml:"version"`
	Servers []Server `yaml:"servers"`
}

type Server struct {
	Name        string `yaml:"name"`
	Address     string `yaml:"address"`
	Description string `yaml:"description,omitempty"`
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
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: currentVersion}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{Version: currentVersion}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	seenNames := make(map[string]struct{}, len(c.Servers))
	for index, server := range c.Servers {
		if err := server.Validate(); err != nil {
			return fmt.Errorf("server %d: %w", index+1, err)
		}

		name := strings.TrimSpace(server.Name)
		if _, exists := seenNames[name]; exists {
			return fmt.Errorf("server %q is duplicated", name)
		}
		seenNames[name] = struct{}{}
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
	return nil
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
	return server
}
