package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	cryptossh "golang.org/x/crypto/ssh"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/security"
)

const scriptedHealthOutput = "###UPTIME###\n 10:00:00 up 3 days,  2:14,  1 user,  load average: 0.10, 0.05, 0.01\n" +
	"###MEM###\n              total        used        free\nMem:           7951        2048        3000\n" +
	"###DISK###\nFilesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   20G   28G  42% /\n" +
	"###DOCKER###\nabc123|prelo-core|prelo-core:latest|Up 2 hours\n"

// 32 random bytes, Base64 — test-only, never used outside this package's tests.
const testCipherKey = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="

// generateClientKeyPair returns an ephemeral ed25519 key pair — the public half for the fake
// server's PublicKeyCallback to accept, and the private half PEM-encoded exactly like what a
// real registration request's privateKeyPem field would contain.
func generateClientKeyPair(t *testing.T) (ed25519.PublicKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	block, err := cryptossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return pub, pem.EncodeToMemory(block)
}

// fakeSSHServer runs a minimal in-process SSH server (golang.org/x/crypto/ssh has a full server
// implementation, not just a client) that accepts exactly one client public key and, on any
// "exec" request, writes back a fixed, scripted response — no Docker, no real network host,
// fully deterministic. Returns the listener address and the server's own host key fingerprint.
func fakeSSHServer(t *testing.T, clientPub ed25519.PublicKey, response string) (addr, hostKeyFingerprint string) {
	t.Helper()
	hostPub, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := cryptossh.NewSignerFromSigner(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}
	wantKey, err := cryptossh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatalf("client public key: %v", err)
	}

	config := &cryptossh.ServerConfig{
		PublicKeyCallback: func(_ cryptossh.ConnMetadata, key cryptossh.PublicKey) (*cryptossh.Permissions, error) {
			if string(key.Marshal()) != string(wantKey.Marshal()) {
				return nil, errors.New("unauthorized key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		nConn, err := listener.Accept()
		if err != nil {
			return
		}
		conn, chans, reqs, err := cryptossh.NewServerConn(nConn, config)
		if err != nil {
			return
		}
		defer conn.Close()
		go cryptossh.DiscardRequests(reqs)
		for newChannel := range chans {
			channel, requests, err := newChannel.Accept()
			if err != nil {
				return
			}
			go func() {
				for req := range requests {
					if req.Type == "exec" {
						_, _ = channel.Write([]byte(response))
						_, _ = channel.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{0}))
						_ = req.Reply(true, nil)
						_ = channel.Close()
					} else {
						_ = req.Reply(false, nil)
					}
				}
			}()
		}
	}()

	hostPubKey, err := cryptossh.NewPublicKey(hostPub)
	if err != nil {
		t.Fatalf("host public key: %v", err)
	}
	return listener.Addr().String(), cryptossh.FingerprintSHA256(hostPubKey)
}

func testServer(t *testing.T, host string, port int, fingerprint string, cipher *security.MfaCipher, privateKeyPEM []byte) domain.Server {
	t.Helper()
	server, err := domain.NewServer("test", host, port, "root", "tester")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	encrypted, err := cipher.Encrypt(privateKeyPEM, []byte(server.ID.String()))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return server.WithCredential(encrypted, fingerprint, time.Now().UTC())
}

func TestChecker_Check_ParsesHealthOutputFromARealSSHSession(t *testing.T) {
	clientPub, clientPriv := generateClientKeyPair(t)
	addr, fingerprint := fakeSSHServer(t, clientPub, scriptedHealthOutput)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cipher, err := security.NewMfaCipher(testCipherKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	server := testServer(t, host, port, fingerprint, cipher, clientPriv)

	checker := NewChecker(cipher, 3*time.Second)
	snapshot, err := checker.Check(context.Background(), server)
	if err != nil {
		t.Fatalf("check returned a Go error, expected a snapshot: %v", err)
	}
	if snapshot.Status != domain.ServerStatusOnline {
		t.Fatalf("expected ONLINE, got %s (error: %s)", snapshot.Status, snapshot.Error)
	}
	if snapshot.MemoryTotalMB != 7951 || snapshot.MemoryUsedMB != 2048 {
		t.Fatalf("unexpected memory parse: used=%d total=%d", snapshot.MemoryUsedMB, snapshot.MemoryTotalMB)
	}
	if snapshot.DiskUsedPercent != 42 {
		t.Fatalf("expected disk 42%%, got %d", snapshot.DiskUsedPercent)
	}
	if len(snapshot.Containers) != 1 || snapshot.Containers[0].Name != "prelo-core" {
		t.Fatalf("expected one parsed container named prelo-core, got %+v", snapshot.Containers)
	}
}

func TestChecker_Check_DetectsAHostKeyThatNoLongerMatches(t *testing.T) {
	clientPub, clientPriv := generateClientKeyPair(t)
	addr, _ := fakeSSHServer(t, clientPub, scriptedHealthOutput)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cipher, err := security.NewMfaCipher(testCipherKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	// A fingerprint that does NOT match the fake server's real host key — simulates the server
	// having been swapped (or a MITM) since registration.
	server := testServer(t, host, port, "SHA256:this-will-never-match", cipher, clientPriv)

	checker := NewChecker(cipher, 3*time.Second)
	snapshot, err := checker.Check(context.Background(), server)
	if err != nil {
		t.Fatalf("check returned a Go error, expected a snapshot: %v", err)
	}
	if snapshot.Status != domain.ServerStatusError {
		t.Fatalf("expected ERROR status on host key mismatch, got %s", snapshot.Status)
	}
	if !strings.Contains(snapshot.Error, "host key mismatch") {
		t.Fatalf("expected a host key mismatch message, got %q", snapshot.Error)
	}
}

func TestRegistrar_Register_CapturesTheServersHostKeyFingerprint(t *testing.T) {
	clientPub, clientPriv := generateClientKeyPair(t)
	addr, expectedFingerprint := fakeSSHServer(t, clientPub, "ok\n")
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	registrar := NewRegistrar(3 * time.Second)
	fingerprint, err := registrar.Register(context.Background(), host, port, "root", clientPriv)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if fingerprint != expectedFingerprint {
		t.Fatalf("expected captured fingerprint %q, got %q", expectedFingerprint, fingerprint)
	}
}
