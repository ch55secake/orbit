package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const askpassSocketEnv = "ORBIT_SSH_ASKPASS_SOCKET"

const maxAskpassPassword = 64 * 1024

type askpassBroker struct {
	listener  net.Listener
	directory string
	socket    string
	done      chan struct{}
	mu        sync.Mutex
	password  []byte
	closed    bool
	close     sync.Once
	closeErr  error
}

func newAskpassBroker(password []byte) (*askpassBroker, error) {
	if len(password) > maxAskpassPassword {
		return nil, errors.New("SSH password exceeds maximum length")
	}
	directory, err := os.MkdirTemp("", "oa-")
	if err != nil {
		return nil, fmt.Errorf("create SSH password socket directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("secure SSH password socket directory: %w", err)
	}
	socket := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("listen on SSH password socket: %w", err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socket)
		_ = os.Remove(directory)
		return nil, fmt.Errorf("secure SSH password socket: %w", err)
	}
	broker := &askpassBroker{
		listener:  listener,
		directory: directory,
		socket:    socket,
		done:      make(chan struct{}),
		password:  append([]byte(nil), password...),
	}
	go broker.serve()
	return broker, nil
}

func newAskpassBrokerIfNeeded(password []byte) (*askpassBroker, error) {
	if password == nil {
		return nil, nil
	}
	return newAskpassBroker(password)
}

func configureAskpass(command *exec.Cmd, broker *askpassBroker) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate Orbit for SSH AskPass: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve Orbit path for SSH AskPass: %w", err)
	}
	env := command.Env
	env = setEnvironment(env, "SSH_ASKPASS", executable)
	env = setEnvironment(env, "SSH_ASKPASS_REQUIRE", "force")
	env = setEnvironment(env, askpassSocketEnv, broker.socket)
	if environmentValue(env, "DISPLAY") == "" {
		env = setEnvironment(env, "DISPLAY", ":0")
	}
	command.Env = env
	return nil
}

func setEnvironment(environment []string, name, value string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	prefix := name + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			filtered = append(filtered, variable)
		}
	}
	return append(filtered, prefix+value)
}

func environmentValue(environment []string, name string) string {
	if environment == nil {
		return os.Getenv(name)
	}
	prefix := name + "="
	for _, variable := range environment {
		if strings.HasPrefix(variable, prefix) {
			return strings.TrimPrefix(variable, prefix)
		}
	}
	return ""
}

func closeBroker(broker *askpassBroker) error {
	if broker == nil {
		return nil
	}
	return broker.Close()
}

func closeProcessAskpass(current *process) error {
	if current.askpass == nil {
		return nil
	}
	err := current.askpass.Close()
	current.askpass = nil
	return err
}

func (b *askpassBroker) serve() {
	defer close(b.done)
	for {
		connection, err := b.listener.Accept()
		if err != nil {
			return
		}
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		b.mu.Lock()
		if b.closed {
			_, _ = connection.Write([]byte{0xff, 0xff, 0xff, 0xff})
		} else {
			var header [4]byte
			binary.BigEndian.PutUint32(header[:], uint32(len(b.password)))
			if writeAll(connection, header[:]) == nil {
				_ = writeAll(connection, b.password)
			}
		}
		b.mu.Unlock()
		_ = connection.Close()
	}
}

func (b *askpassBroker) Close() error {
	b.close.Do(func() {
		if err := b.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			b.closeErr = err
		}
		<-b.done
		b.mu.Lock()
		b.closed = true
		clear(b.password)
		b.password = nil
		b.mu.Unlock()
		if err := os.Remove(b.socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.closeErr = errors.Join(b.closeErr, err)
		}
		if err := os.Remove(b.directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.closeErr = errors.Join(b.closeErr, err)
		}
	})
	return b.closeErr
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

// IsAskpassHelper reports whether this Orbit process was launched by OpenSSH
// as its password-output helper.
func IsAskpassHelper() bool {
	return os.Getenv(askpassSocketEnv) != ""
}

// RunAskpassHelper is called by the Orbit executable when OpenSSH invokes it
// through SSH_ASKPASS. The password travels only over the private socket and
// standard output; it is never included in process arguments or environment.
func RunAskpassHelper() error {
	socket := os.Getenv(askpassSocketEnv)
	if socket == "" {
		return errors.New("missing SSH askpass socket")
	}
	return runAskpassHelper(socket, os.Stdout)
}

func runAskpassHelper(socket string, output io.Writer) error {
	password, err := readAskpassPassword(socket)
	if err != nil {
		return err
	}
	defer clear(password)
	if err := writeAll(output, password); err != nil {
		return err
	}
	_, err = io.WriteString(output, "\n")
	return err
}

func readAskpassPassword(socket string) ([]byte, error) {
	connection, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to SSH password socket: %w", err)
	}
	defer func() { _ = connection.Close() }()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	var header [4]byte
	if _, err := io.ReadFull(connection, header[:]); err != nil {
		return nil, fmt.Errorf("read SSH password socket: %w", err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > maxAskpassPassword {
		return nil, errors.New("SSH password exceeds maximum length")
	}
	password := make([]byte, length)
	if _, err := io.ReadFull(connection, password); err != nil {
		clear(password)
		return nil, fmt.Errorf("read SSH password socket: %w", err)
	}
	return password, nil
}
