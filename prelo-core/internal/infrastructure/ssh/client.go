// Package ssh implements Fase S1's direct outbound SSH connection to a registered Server — no
// tunnel yet, prelo-core dials the host itself. See application.ServerHealthChecker/
// ServerRegistrationDialer for the ports this satisfies.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/security"
)

// DefaultTimeout bounds one whole health check (dial + handshake + command) — a hung remote
// must never hold an HTTP handler goroutine open indefinitely.
const DefaultTimeout = 8 * time.Second

const healthScript = `set -e
echo "###UPTIME###"
uptime
echo "###MEM###"
free -m
echo "###DISK###"
df -h /
echo "###DOCKER###"
docker ps --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}' 2>/dev/null || echo "DOCKER_UNAVAILABLE"
`

// HostKeyMismatchError means a server's live host key no longer matches the fingerprint
// captured at registration (trust-on-first-use) — a legitimate key rotation or something worse;
// either way a human must re-register, this is never auto-healed.
type HostKeyMismatchError struct{ Expected, Got string }

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("host key mismatch: expected %s, got %s", e.Expected, e.Got)
}

// Checker implements application.ServerHealthChecker.
type Checker struct {
	cipher  *security.MfaCipher
	timeout time.Duration
}

func NewChecker(cipher *security.MfaCipher, timeout time.Duration) *Checker {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Checker{cipher: cipher, timeout: timeout}
}

func (c *Checker) Check(ctx context.Context, server domain.Server) (application.ServerHealthSnapshot, error) {
	plainKey, err := c.cipher.Decrypt(server.EncryptedPrivateKey, []byte(server.ID.String()))
	if err != nil {
		return offline("stored credential could not be decrypted"), nil
	}
	signer, err := ssh.ParsePrivateKey(plainKey)
	if err != nil {
		return offline("stored credential is not a valid private key"), nil
	}

	config := &ssh.ClientConfig{
		User:            server.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: fixedFingerprintCallback(server.HostKeyFingerprint),
		Timeout:         c.timeout,
	}

	conn, err := dial(ctx, server.Host, server.SSHPort, config, c.timeout)
	if err != nil {
		var mismatch *HostKeyMismatchError
		if errors.As(err, &mismatch) {
			return application.ServerHealthSnapshot{Status: domain.ServerStatusError, Error: mismatch.Error()}, nil
		}
		return offline(classifyDialError(err)), nil
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return offline("ssh session could not be opened"), nil
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Run(healthScript) }()
	select {
	case runErr := <-done:
		if runErr != nil {
			return offline("health script failed: " + runErr.Error()), nil
		}
	case <-runCtx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return offline("health check timed out"), nil
	}

	snapshot := parseHealthOutput(stdout.Bytes())
	snapshot.Status = domain.ServerStatusOnline
	return snapshot, nil
}

// Registrar implements application.ServerRegistrationDialer — the ONE place a host key is ever
// accepted without comparison (trust-on-first-use), and only while registering a brand-new
// Server; Checker's ongoing health-check path never uses this.
type Registrar struct{ timeout time.Duration }

func NewRegistrar(timeout time.Duration) *Registrar {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Registrar{timeout: timeout}
}

func (r *Registrar) Register(ctx context.Context, host string, port int, user string, privateKeyPEM []byte) (string, error) {
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("private key is not valid: %w", err)
	}

	var capturedFingerprint string
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			capturedFingerprint = ssh.FingerprintSHA256(key)
			return nil
		},
		Timeout: r.timeout,
	}

	conn, err := dial(ctx, host, port, config, r.timeout)
	if err != nil {
		return "", fmt.Errorf("could not connect: %w", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session could not be opened: %w", err)
	}
	defer session.Close()
	if err := session.Run("true"); err != nil {
		return "", fmt.Errorf("credential did not authenticate: %w", err)
	}
	return capturedFingerprint, nil
}

func dial(ctx context.Context, host string, port int, config *ssh.ClientConfig, timeout time.Duration) (*ssh.Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: timeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = tcpConn.SetDeadline(time.Now().Add(timeout))
	clientConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, config)
	if err != nil {
		_ = tcpConn.Close()
		return nil, err
	}
	_ = tcpConn.SetDeadline(time.Time{})
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func fixedFingerprintCallback(expected string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != expected {
			return &HostKeyMismatchError{Expected: expected, Got: got}
		}
		return nil
	}
}

func classifyDialError(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "i/o timeout"):
		return "connection timed out"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "no route to host"):
		return "no route to host"
	case strings.Contains(msg, "unable to authenticate"):
		return "authentication failed"
	default:
		return "could not connect"
	}
}

func offline(reason string) application.ServerHealthSnapshot {
	return application.ServerHealthSnapshot{Status: domain.ServerStatusOffline, Error: reason}
}

func parseHealthOutput(output []byte) application.ServerHealthSnapshot {
	sections := splitSections(string(output))
	snapshot := application.ServerHealthSnapshot{}
	snapshot.Uptime = strings.TrimSpace(sections["UPTIME"])
	snapshot.MemoryUsedMB, snapshot.MemoryTotalMB = parseFreeOutput(sections["MEM"])
	snapshot.DiskUsedPercent = parseDiskOutput(sections["DISK"])
	snapshot.Containers = parseDockerOutput(sections["DOCKER"])
	return snapshot
}

func splitSections(output string) map[string]string {
	sections := map[string]string{}
	var current string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, "###") && strings.HasSuffix(trimmed, "###") && len(trimmed) > 6 {
			current = strings.Trim(trimmed, "#")
			continue
		}
		if current != "" {
			sections[current] += trimmed + "\n"
		}
	}
	return sections
}

func parseFreeOutput(section string) (usedMB, totalMB int) {
	for _, line := range strings.Split(strings.TrimSpace(section), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "Mem:") {
			totalMB, _ = strconv.Atoi(fields[1])
			usedMB, _ = strconv.Atoi(fields[2])
			return
		}
	}
	return 0, 0
}

func parseDiskOutput(section string) int {
	lines := strings.Split(strings.TrimSpace(section), "\n")
	if len(lines) < 2 {
		return 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
	return n
}

func parseDockerOutput(section string) []application.ContainerStatus {
	trimmed := strings.TrimSpace(section)
	if trimmed == "" || trimmed == "DOCKER_UNAVAILABLE" {
		return nil
	}
	var containers []application.ContainerStatus
	for _, line := range strings.Split(trimmed, "\n") {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		containers = append(containers, application.ContainerStatus{ID: parts[0], Name: parts[1], Image: parts[2], Status: parts[3]})
	}
	return containers
}
