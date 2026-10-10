package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	cfg.Version = currentVersion
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

func (c *Config) AddServer(server Server) error {
	if server.Name == "" {
		return errors.New("server name must not be empty")
	}
	if server.Address == "" {
		return errors.New("server address must not be empty")
	}
	for _, existing := range c.Servers {
		if existing.Name == server.Name {
			return fmt.Errorf("server %q already exists", server.Name)
		}
	}
	c.Servers = append(c.Servers, server)
	return nil
}
