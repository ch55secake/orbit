package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestServiceTokenPrivateFile(t *testing.T) {
	for _, test := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"private", testToken + "\n", 0600, true}, {"readonly", testToken, 0400, true},
		{"group", testToken, 0640, false}, {"public", testToken, 0644, false},
		{"short", "short", 0600, false}, {"control", testToken + "\x1b", 0600, false},
		{"space", testToken + " x", 0600, false}, {"large", strings.Repeat("x", 4097), 0600, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte(test.body), test.mode); err != nil {
				t.Fatal(err)
			}
			token, err := ServiceToken(path)
			if (err == nil) != test.valid {
				t.Fatalf("valid %v: %v", test.valid, err)
			}
			if test.valid && token != testToken {
				t.Fatal("token changed")
			}
			if err != nil && strings.Contains(err.Error(), testToken) {
				t.Fatal("token leaked")
			}
		})
	}
	if _, err := ServiceToken(""); err == nil {
		t.Fatal("missing path accepted")
	}
	if _, err := ServiceToken(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testToken), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceToken(link); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestServiceLogTokenUsesPrivateFilePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log-token")
	if err := os.WriteFile(path, []byte(testToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := ServiceLogToken(path)
	if err != nil || token != testToken {
		t.Fatalf("service log token: %q %v", token, err)
	}
	if _, err := ServiceLogToken(""); err == nil || !strings.Contains(err.Error(), "service_log_token_file") {
		t.Fatalf("missing log token setting was not actionable: %v", err)
	}
}

func TestServiceRestartTokenUsesPrivateFilePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart-token")
	if err := os.WriteFile(path, []byte(testToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := ServiceRestartToken(path)
	if err != nil || token != testToken {
		t.Fatalf("service restart token: %q %v", token, err)
	}
	if _, err := ServiceRestartToken(""); err == nil || !strings.Contains(err.Error(), "service_restart_token_file") {
		t.Fatalf("missing restart token setting was not actionable: %v", err)
	}
}
