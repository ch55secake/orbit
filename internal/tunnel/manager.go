package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ch55secake/orbit/internal/config"
)

const (
	defaultSSHPath   = "ssh"
	startupTimeout   = 10 * time.Second
	startupPoll      = 25 * time.Millisecond
	startupProbeTime = 50 * time.Millisecond
	maxStderrSize    = 4096
)

var ErrPasswordRequired = errors.New("SSH password authentication required")

// Manager owns the SSH processes used to forward configured Stardust servers.
// A manager is intended to be scoped to one status command or TUI instance.
type Manager struct {
	mu          sync.Mutex
	processes   map[string]*process
	sshPath     string
	startupWait time.Duration
	command     func(string, ...string) *exec.Cmd
	closed      bool
}

type process struct {
	command *exec.Cmd
	stderr  *limitedBuffer
	askpass *askpassBroker
	done    chan struct{}
	err     error
}

func NewManager() *Manager {
	return &Manager{
		processes:   make(map[string]*process),
		sshPath:     defaultSSHPath,
		startupWait: startupTimeout,
		command:     exec.Command,
	}
}

// Ensure starts a tunnel if one is not already running. A process that exited
// since the previous call is treated as disconnected and started again.
func (m *Manager) Ensure(ctx context.Context, server config.Server) error {
	if server.SSH == nil {
		return nil
	}
	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid SSH tunnel configuration: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("SSH tunnel manager is closed")
	}
	if current := m.processes[server.Name]; current != nil {
		select {
		case <-current.done:
			delete(m.processes, server.Name)
		default:
			return nil
		}
	}
	return m.startLocked(ctx, server, nil)
}

// EnsureWithPassword retries a server tunnel using OpenSSH AskPass. The
// password is copied into a short-lived in-memory broker and cleared as soon
// as SSH authentication completes or startup fails.
func (m *Manager) EnsureWithPassword(ctx context.Context, server config.Server, password []byte) error {
	if server.SSH == nil {
		return nil
	}
	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid SSH tunnel configuration: %w", err)
	}
	if password == nil {
		return errors.New("SSH password must not be nil")
	}
	defer clear(password)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("SSH tunnel manager is closed")
	}
	if current := m.processes[server.Name]; current != nil {
		select {
		case <-current.done:
			delete(m.processes, server.Name)
		default:
			return nil
		}
	}
	return m.startLocked(ctx, server, password)
}

// Stop terminates one server's managed tunnel if it is running.
func (m *Manager) Stop(server config.Server) error {
	if server.SSH == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.processes[server.Name]; current != nil {
		return m.stopLocked(server.Name, current)
	}
	return nil
}

// Reconnect forcibly replaces the server's existing tunnel.
func (m *Manager) Reconnect(ctx context.Context, server config.Server) error {
	if server.SSH == nil {
		return nil
	}
	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid SSH tunnel configuration: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("SSH tunnel manager is closed")
	}
	if current := m.processes[server.Name]; current != nil {
		if err := m.stopLocked(server.Name, current); err != nil {
			return err
		}
	}
	return m.startLocked(ctx, server, nil)
}

// Close terminates and reaps all child ssh processes. It is safe to call more
// than once.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	var closeErrors []error
	for name, current := range m.processes {
		if err := m.stopLocked(name, current); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

func (m *Manager) startLocked(ctx context.Context, server config.Server, password []byte) error {
	address, err := url.Parse(server.Address)
	if err != nil {
		return fmt.Errorf("parse SSH tunnel address: %w", err)
	}
	localAddress := net.JoinHostPort(address.Hostname(), address.Port())
	listener, err := net.Listen("tcp", localAddress)
	if err != nil {
		return fmt.Errorf("SSH tunnel local port %s is unavailable: %w", localAddress, err)
	}
	if err := listener.Close(); err != nil {
		return fmt.Errorf("release SSH tunnel local port %s: %w", localAddress, err)
	}

	broker, err := newAskpassBrokerIfNeeded(password)
	if err != nil {
		return err
	}
	args, err := sshArgs(server, address, broker != nil)
	if err != nil {
		return errors.Join(err, closeBroker(broker))
	}
	command := m.command(m.sshPath, args...)
	command.Stdout = io.Discard
	stderr := &limitedBuffer{limit: maxStderrSize}
	command.Stderr = stderr
	if broker != nil {
		if err := configureAskpass(command, broker); err != nil {
			return errors.Join(err, closeBroker(broker))
		}
	}
	if err := command.Start(); err != nil {
		return errors.Join(fmt.Errorf("start SSH tunnel for server %q: %w", server.Name, err), closeBroker(broker))
	}
	current := &process{command: command, stderr: stderr, askpass: broker, done: make(chan struct{})}
	m.processes[server.Name] = current
	go func() {
		current.err = command.Wait()
		close(current.done)
	}()

	timer := time.NewTimer(m.startupWait)
	defer timer.Stop()
	ticker := time.NewTicker(startupPoll)
	defer ticker.Stop()
	for {
		select {
		case <-current.done:
			delete(m.processes, server.Name)
			return errors.Join(startupError(server.Name, current), closeProcessAskpass(current))
		case <-ctx.Done():
			stopErr := m.stopLocked(server.Name, current)
			return errors.Join(fmt.Errorf("start SSH tunnel for server %q: %w", server.Name, ctx.Err()), stopErr)
		case <-timer.C:
			stopErr := m.stopLocked(server.Name, current)
			waitErr := fmt.Errorf("SSH tunnel for server %q did not open local endpoint %s within %s", server.Name, localAddress, m.startupWait)
			return errors.Join(waitErr, stopErr)
		default:
		}

		connection, dialErr := net.DialTimeout("tcp", localAddress, startupProbeTime)
		if dialErr == nil {
			_ = connection.Close()
			select {
			case <-current.done:
				delete(m.processes, server.Name)
				return errors.Join(startupError(server.Name, current), closeProcessAskpass(current))
			default:
				if err := closeProcessAskpass(current); err != nil {
					return errors.Join(fmt.Errorf("clear SSH password for server %q: %w", server.Name, err), m.stopLocked(server.Name, current))
				}
				return nil
			}
		}

		select {
		case <-current.done:
			delete(m.processes, server.Name)
			return errors.Join(startupError(server.Name, current), closeProcessAskpass(current))
		case <-ctx.Done():
			stopErr := m.stopLocked(server.Name, current)
			return errors.Join(fmt.Errorf("start SSH tunnel for server %q: %w", server.Name, ctx.Err()), stopErr)
		case <-timer.C:
			stopErr := m.stopLocked(server.Name, current)
			waitErr := fmt.Errorf("SSH tunnel for server %q did not open local endpoint %s within %s", server.Name, localAddress, m.startupWait)
			return errors.Join(waitErr, stopErr)
		case <-ticker.C:
		}
	}
}

func (m *Manager) stopLocked(name string, current *process) error {
	select {
	case <-current.done:
		delete(m.processes, name)
		return closeProcessAskpass(current)
	default:
	}

	if err := current.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return errors.Join(fmt.Errorf("stop SSH tunnel for server %q: %w", name, err), closeProcessAskpass(current))
	}
	<-current.done
	delete(m.processes, name)
	return closeProcessAskpass(current)
}

func startupError(name string, current *process) error {
	message := strings.TrimSpace(current.stderr.String())
	if authenticationRejected(message) {
		return fmt.Errorf("SSH authentication rejected for server %q (%s): %w", name, message, ErrPasswordRequired)
	}
	if message != "" {
		return fmt.Errorf("SSH tunnel for server %q exited during startup (%v): %s", name, current.err, message)
	}
	if current.err == nil {
		return fmt.Errorf("SSH tunnel for server %q exited during startup", name)
	}
	return fmt.Errorf("SSH tunnel for server %q exited during startup: %w", name, current.err)
}

func authenticationRejected(message string) bool {
	lowerMessage := strings.ToLower(message)
	return strings.Contains(lowerMessage, "permission denied") ||
		strings.Contains(lowerMessage, "no more authentication methods to try") ||
		strings.Contains(lowerMessage, "authentication failed")
}

func sshArgs(server config.Server, address *url.URL, passwordAuth bool) ([]string, error) {
	settings := server.SSH
	sshPort := settings.Port
	if sshPort == 0 {
		sshPort = 22
	}
	remoteHost := strings.TrimSpace(settings.RemoteHost)
	if remoteHost == "" {
		remoteHost = "127.0.0.1"
	}
	remotePort := settings.RemotePort
	localHost := address.Hostname()
	localPort := address.Port()
	forward := fmt.Sprintf("%s:%s:%s:%d", forwardHost(localHost), localPort, forwardHost(remoteHost), remotePort)

	batchMode := "BatchMode=yes"
	if passwordAuth {
		batchMode = "BatchMode=no"
	}
	args := []string{
		"-N",
		"-o", batchMode,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "RequestTTY=no",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ConnectTimeout=8",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-p", strconv.Itoa(sshPort),
	}
	if passwordAuth {
		args = append(args,
			"-o", "NumberOfPasswordPrompts=1",
			"-o", "PreferredAuthentications=publickey,password",
		)
	}
	if user := strings.TrimSpace(settings.User); user != "" {
		args = append(args, "-l", user)
	}
	if identity := strings.TrimSpace(settings.IdentityFile); identity != "" {
		if strings.HasPrefix(identity, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("resolve SSH identity file: %w", err)
			}
			identity = filepath.Join(home, strings.TrimPrefix(identity, "~/"))
		}
		args = append(args, "-i", identity)
	}
	args = append(args, "-L", forward, "--", strings.TrimSpace(settings.Host))
	return args, nil
}

func forwardHost(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

type limitedBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	length := len(data)
	b.data = append(b.data, data...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return length, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
