package config

import (
	"path/filepath"
	"testing"
)

func TestServiceCredentialReferencesAreOptionalAndAbsolute(t *testing.T) {
	server := Server{Name: "local", Address: "http://127.0.0.1:8787"}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "token")} {
		server.ServiceTokenFile = path
		if err := server.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"relative-token", "/secret\nfile"} {
		server.ServiceTokenFile = path
		if err := server.Validate(); err == nil {
			t.Fatal("unsafe credential path accepted")
		}
	}
}

func TestServiceLogCredentialReferenceIsOptionalAndAbsolute(t *testing.T) {
	server := Server{Name: "local", Address: "http://127.0.0.1:8787"}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "log-token")} {
		server.ServiceLogTokenFile = path
		if err := server.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"relative-token", "/secret\nfile"} {
		server.ServiceLogTokenFile = path
		if err := server.Validate(); err == nil {
			t.Fatal("unsafe log credential path accepted")
		}
	}
}
