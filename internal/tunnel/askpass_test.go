package tunnel

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAskpassBrokerDeliversPasswordAndCleansUp(t *testing.T) {
	secret := []byte("session-only secret")
	broker, err := newAskpassBroker(secret)
	if err != nil {
		t.Fatalf("create askpass broker: %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("close askpass broker: %v", err)
		}
	}()

	directoryInfo, err := os.Stat(broker.directory)
	if err != nil {
		t.Fatalf("stat askpass directory: %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("askpass directory permissions = %04o, want 0700", got)
	}
	socketInfo, err := os.Stat(broker.socket)
	if err != nil {
		t.Fatalf("stat askpass socket: %v", err)
	}
	if got := socketInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("askpass socket permissions = %04o, want 0600", got)
	}

	var output bytes.Buffer
	if err := runAskpassHelper(broker.socket, &output); err != nil {
		t.Fatalf("run askpass helper: %v", err)
	}
	if got, want := output.String(), string(secret)+"\n"; got != want {
		t.Fatalf("askpass output = %q, want %q", got, want)
	}

	if err := broker.Close(); err != nil {
		t.Fatalf("close askpass broker: %v", err)
	}
	if _, err := os.Stat(broker.directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("askpass directory still exists after close: %v", err)
	}
	if len(broker.password) != 0 {
		t.Fatal("broker retained password after close")
	}
}

func TestConfigureAskpassUsesSocketAndNoPasswordEnvironment(t *testing.T) {
	broker, err := newAskpassBroker([]byte("never in environment"))
	if err != nil {
		t.Fatalf("create askpass broker: %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("close askpass broker: %v", err)
		}
	}()

	command := exec.Command("ssh")
	command.Env = []string{"PATH=/usr/bin"}
	if err := configureAskpass(command, broker); err != nil {
		t.Fatalf("configure askpass: %v", err)
	}
	joined := strings.Join(command.Env, "\n")
	if !strings.Contains(joined, "SSH_ASKPASS=") || !strings.Contains(joined, "SSH_ASKPASS_REQUIRE=force") || !strings.Contains(joined, askpassSocketEnv+"="+broker.socket) {
		t.Fatalf("askpass environment missing expected settings: %q", joined)
	}
	if strings.Contains(joined, "never in environment") {
		t.Fatal("password was included in child environment")
	}
}

func TestPasswordSSHArgsEnableAskpassOnlyOnRetry(t *testing.T) {
	server := testTunnelServer(t)
	address, err := url.Parse(server.Address)
	if err != nil {
		t.Fatal(err)
	}
	args, err := sshArgs(server, address, true)
	if err != nil {
		t.Fatalf("password SSH args: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"BatchMode=no", "NumberOfPasswordPrompts=1", "PreferredAuthentications=publickey,password"} {
		if !strings.Contains(joined, want) {
			t.Errorf("password SSH args %q do not contain %q", joined, want)
		}
	}
}

func TestAuthenticationRejected(t *testing.T) {
	for _, message := range []string{
		"prod: Permission denied (publickey).",
		"No more authentication methods to try.",
		"authentication failed",
	} {
		if !authenticationRejected(message) {
			t.Errorf("authenticationRejected(%q) = false", message)
		}
	}
	for _, message := range []string{"Host key verification failed.", "Connection timed out"} {
		if authenticationRejected(message) {
			t.Errorf("authenticationRejected(%q) = true", message)
		}
	}
}

func TestSetEnvironmentReplacesValue(t *testing.T) {
	env := setEnvironment([]string{"SSH_ASKPASS=old", "KEEP=yes"}, "SSH_ASKPASS", filepath.Join("/tmp", "orbit"))
	if got := strings.Join(env, "\n"); strings.Count(got, "SSH_ASKPASS=") != 1 || !strings.Contains(got, "SSH_ASKPASS=/tmp/orbit") {
		t.Fatalf("updated environment = %q", got)
	}
}
