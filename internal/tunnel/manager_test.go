package tunnel

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ch55secake/orbit/internal/config"
)

func TestSSHArgsUseOnlyDedicatedForwardingOptions(t *testing.T) {
	server := testTunnelServer(t)
	address, err := url.Parse(server.Address)
	if err != nil {
		t.Fatal(err)
	}
	args, err := sshArgs(server, address)
	if err != nil {
		t.Fatalf("ssh args: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-N", "ExitOnForwardFailure=yes", "BatchMode=yes", "-L 127.0.0.1:" + address.Port() + ":127.0.0.1:8787", "-- prod"} {
		if !strings.Contains(joined, want) {
			t.Errorf("SSH args %q do not contain %q", joined, want)
		}
	}
	if strings.Contains(joined, "ProxyCommand") || len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != "prod" {
		t.Fatalf("SSH args include an unexpected command: %q", joined)
	}
}

func TestManagerStartsReconnectsAndStopsSSHProcess(t *testing.T) {
	manager, starts := helperManager(t, "hold", time.Second)
	server := testTunnelServer(t)

	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("ensure tunnel: %v", err)
	}
	if got := starts(); got != 1 {
		t.Fatalf("SSH process starts = %d, want 1", got)
	}
	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("ensure existing tunnel: %v", err)
	}
	if got := starts(); got != 1 {
		t.Fatalf("SSH process starts after repeated ensure = %d, want 1", got)
	}
	if err := manager.Reconnect(context.Background(), server); err != nil {
		t.Fatalf("reconnect tunnel: %v", err)
	}
	if got := starts(); got != 2 {
		t.Fatalf("SSH process starts after reconnect = %d, want 2", got)
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if len(manager.processes) != 0 {
		t.Fatalf("processes remain after close: %+v", manager.processes)
	}
	if err := manager.Ensure(context.Background(), server); err == nil || !strings.Contains(err.Error(), "manager is closed") {
		t.Fatalf("ensure after close error = %v, want closed manager", err)
	}
	if got := starts(); got != 2 {
		t.Fatalf("SSH process starts after manager close = %d, want 2", got)
	}
}

func TestManagerRestartsDisconnectedSSHProcess(t *testing.T) {
	manager, starts := helperManager(t, "hold", time.Second)
	server := testTunnelServer(t)
	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("ensure tunnel: %v", err)
	}

	manager.mu.Lock()
	current := manager.processes[server.Name]
	manager.mu.Unlock()
	if err := current.command.Process.Kill(); err != nil {
		t.Fatalf("simulate SSH disconnect: %v", err)
	}
	select {
	case <-current.done:
	case <-time.After(time.Second):
		t.Fatal("disconnected SSH helper did not exit")
	}

	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("ensure after disconnect: %v", err)
	}
	if got := starts(); got != 2 {
		t.Fatalf("SSH process starts after disconnect = %d, want 2", got)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
}

func TestManagerStopsOneTunnelAndCanStartItAgain(t *testing.T) {
	manager, starts := helperManager(t, "hold", time.Second)
	server := testTunnelServer(t)
	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("ensure tunnel: %v", err)
	}
	current := manager.processes[server.Name]
	if err := manager.Stop(server); err != nil {
		t.Fatalf("stop tunnel: %v", err)
	}
	select {
	case <-current.done:
	default:
		t.Fatal("SSH process is still running after Stop")
	}
	if _, exists := manager.processes[server.Name]; exists {
		t.Fatal("stopped SSH process remains registered")
	}
	if err := manager.Ensure(context.Background(), server); err != nil {
		t.Fatalf("start tunnel after Stop: %v", err)
	}
	if got := starts(); got != 2 {
		t.Fatalf("SSH process starts = %d, want 2", got)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
}

func TestManagerReportsStartupAndLocalPortErrors(t *testing.T) {
	t.Run("ssh startup", func(t *testing.T) {
		manager, _ := helperManager(t, "fail", time.Second)
		err := manager.Ensure(context.Background(), testTunnelServer(t))
		if err == nil || !strings.Contains(err.Error(), "Address already in use") {
			t.Fatalf("startup error = %v, want SSH stderr", err)
		}
	})

	t.Run("local port already in use", func(t *testing.T) {
		server := testTunnelServer(t)
		listener, err := net.Listen("tcp", strings.TrimPrefix(server.Address, "http://"))
		if err != nil {
			t.Fatalf("occupy local port: %v", err)
		}
		defer func() {
			if err := listener.Close(); err != nil {
				t.Errorf("close occupied port listener: %v", err)
			}
		}()

		manager, starts := helperManager(t, "hold", time.Second)
		err = manager.Ensure(context.Background(), server)
		if err == nil || !strings.Contains(err.Error(), "local port") {
			t.Fatalf("local port error = %v, want local port conflict", err)
		}
		if got := starts(); got != 0 {
			t.Fatalf("SSH process starts = %d, want 0 for occupied local port", got)
		}
	})
}

func TestSSHProcessHelper(t *testing.T) {
	if os.Getenv("ORBIT_SSH_TEST_HELPER") != "1" {
		return
	}
	switch os.Getenv("ORBIT_SSH_TEST_MODE") {
	case "hold":
		listener, err := net.Listen("tcp", os.Getenv("ORBIT_SSH_TEST_LOCAL_ADDR"))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			if err := connection.Close(); err != nil {
				t.Errorf("close helper connection: %v", err)
			}
		}
	case "fail":
		_, _ = fmt.Fprintln(os.Stderr, "bind [127.0.0.1]:18787: Address already in use")
		os.Exit(255)
	default:
		os.Exit(2)
	}
}

func helperManager(t *testing.T, mode string, startupWait time.Duration) (*Manager, func() int) {
	t.Helper()
	manager := NewManager()
	manager.startupWait = startupWait
	var mu sync.Mutex
	starts := 0
	manager.command = func(_ string, args ...string) *exec.Cmd {
		mu.Lock()
		starts++
		mu.Unlock()
		command := exec.Command(os.Args[0], "-test.run=^TestSSHProcessHelper$")
		command.Env = append(os.Environ(), "ORBIT_SSH_TEST_HELPER=1", "ORBIT_SSH_TEST_MODE="+mode, "ORBIT_SSH_TEST_LOCAL_ADDR="+localAddressFromArgs(args))
		return command
	}
	return manager, func() int {
		mu.Lock()
		defer mu.Unlock()
		return starts
	}
}

func localAddressFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-L" && i+1 < len(args) {
			forward := strings.SplitN(args[i+1], ":", 4)
			if len(forward) == 4 {
				return net.JoinHostPort(strings.Trim(forward[0], "[]"), forward[1])
			}
		}
	}
	return "127.0.0.1:0"
}

func testTunnelServer(t *testing.T) config.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release test port: %v", err)
	}
	return config.Server{
		Name:    "remote",
		Address: "http://127.0.0.1:" + strconv.Itoa(port),
		SSH:     &config.SSHConfig{Host: "prod", User: "orbit", RemotePort: 8787},
	}
}
