package application

import (
	"context"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// ServerCredentialCipher is the narrow port over security.MfaCipher's Encrypt/Decrypt that
// RegisterServerUseCase needs — a second instance of that same AES-GCM type, keyed by a
// dedicated PRELO_SERVER_CREDENTIALS_KEY, never the dashboard TOTP key (see cmd/prelo/main.go).
type ServerCredentialCipher interface {
	Encrypt(plaintext []byte, aad []byte) ([]byte, error)
}

// RegisterServerUseCase validates a new Server's credential end to end — including an actual SSH
// dial — before persisting anything. A Server row only ever exists for a credential that was
// proven to work at registration time.
type RegisterServerUseCase struct {
	servers ServerRepository
	dialer  ServerRegistrationDialer
	cipher  ServerCredentialCipher
}

func NewRegisterServerUseCase(servers ServerRepository, dialer ServerRegistrationDialer, cipher ServerCredentialCipher) *RegisterServerUseCase {
	return &RegisterServerUseCase{servers: servers, dialer: dialer, cipher: cipher}
}

func (uc *RegisterServerUseCase) Register(ctx context.Context, name, host string, sshPort int, sshUser string, privateKeyPEM []byte, createdBy string, projectID *domain.ProjectID) (domain.Server, error) {
	server, err := domain.NewServer(name, host, sshPort, sshUser, createdBy)
	if err != nil {
		return domain.Server{}, err
	}
	server = server.WithProject(projectID)

	fingerprint, err := uc.dialer.Register(ctx, host, sshPort, sshUser, privateKeyPEM)
	if err != nil {
		return domain.Server{}, &domain.ValidationError{Message: "could not connect with the given credential: " + err.Error()}
	}

	encrypted, err := uc.cipher.Encrypt(privateKeyPEM, []byte(server.ID.String()))
	if err != nil {
		return domain.Server{}, err
	}
	server = server.WithCredential(encrypted, fingerprint, time.Now().UTC())

	if err := uc.servers.Insert(ctx, server); err != nil {
		return domain.Server{}, err
	}
	return server, nil
}
