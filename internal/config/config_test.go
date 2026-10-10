package config

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orbit", "config.yaml")
	want := Config{Version: 1, Servers: []Server{{Name: "local", Address: "http://127.0.0.1:8787"}}}
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Servers) != 1 || got.Servers[0] != want.Servers[0] {
		t.Fatalf("loaded config = %+v, want %+v", got, want)
	}
}

func TestMissingConfigIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	if cfg.Version != 1 || len(cfg.Servers) != 0 {
		t.Fatalf("unexpected empty config: %+v", cfg)
	}
}
