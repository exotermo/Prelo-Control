package domain

import "time"

// ServerCredentialKind enumerates the ways a Server's credential can be stored — intentionally
// just one option today: a plaintext password is never an accepted credential kind.
type ServerCredentialKind string

const ServerCredentialSSHPrivateKey ServerCredentialKind = "SSH_PRIVATE_KEY"

type ServerStatus string

const (
	ServerStatusUnknown ServerStatus = "UNKNOWN"
	ServerStatusOnline  ServerStatus = "ONLINE"
	ServerStatusOffline ServerStatus = "OFFLINE"
	ServerStatusError   ServerStatus = "ERROR"
)

const (
	MaxServerNameLength = 120
	MaxServerHostLength = 255
)

// Server is a registered remote machine hermes-go can SSH into (directly, no tunnel — Fase S1)
// to report health and Docker container status. EncryptedPrivateKey is never decrypted except
// for the duration of one SSH dial (see infrastructure/ssh.Checker) and never leaves the backend
// — the API layer's response DTO must never include it. HostKeyFingerprint is captured by
// trust-on-first-use at registration (RegisterServerUseCase) and fixed from then on: every later
// connection compares against it and fails loudly on a mismatch instead of silently trusting
// whatever key answers (host key rotation on the real server requires explicitly re-registering).
type Server struct {
	ID ServerID
	// ProjectID scopes this server to one Fase W project; nil means the "unassigned" bucket
	// (every server registered before Fase W, or registered with no project context selected).
	ProjectID           *ProjectID
	Name                string
	Host                string
	SSHPort             int
	SSHUser             string
	CredentialKind      ServerCredentialKind
	EncryptedPrivateKey []byte
	HostKeyFingerprint  string
	HostKeyCapturedAt   time.Time
	LastStatus          ServerStatus
	LastCheckedAt       *time.Time
	LastError           *string
	CreatedAt           time.Time
	CreatedBy           string
	Version             int64
}

func NewServer(name, host string, sshPort int, sshUser, createdBy string) (Server, error) {
	if isBlank(name) || len(name) > MaxServerNameLength {
		return Server{}, &ValidationError{Message: "server name is required and must be at most 120 characters"}
	}
	if isBlank(host) || len(host) > MaxServerHostLength {
		return Server{}, &ValidationError{Message: "server host is required and must be at most 255 characters"}
	}
	if sshPort < 1 || sshPort > 65535 {
		return Server{}, &ValidationError{Message: "ssh port must be between 1 and 65535"}
	}
	if isBlank(sshUser) {
		return Server{}, &ValidationError{Message: "ssh user is required"}
	}
	return Server{
		ID:             NewServerID(),
		Name:           name,
		Host:           host,
		SSHPort:        sshPort,
		SSHUser:        sshUser,
		CredentialKind: ServerCredentialSSHPrivateKey,
		LastStatus:     ServerStatusUnknown,
		CreatedAt:      time.Now().UTC(),
		CreatedBy:      createdBy,
	}, nil
}

// WithProject assigns this server to a Fase W project — nil leaves it in the "unassigned"
// bucket, same convention as Task.ProjectID.
func (s Server) WithProject(projectID *ProjectID) Server {
	s.ProjectID = projectID
	return s
}

// WithCredential attaches the encrypted private key and the host key fingerprint captured during
// the registration dial — split out from NewServer because both are only known after that dial
// actually succeeds (RegisterServerUseCase never persists a Server whose credential doesn't work).
func (s Server) WithCredential(encryptedPrivateKey []byte, hostKeyFingerprint string, capturedAt time.Time) Server {
	s.EncryptedPrivateKey = encryptedPrivateKey
	s.HostKeyFingerprint = hostKeyFingerprint
	s.HostKeyCapturedAt = capturedAt
	return s
}

// WithHealthResult records the outcome of a health check — called whether it succeeded or failed,
// so a server that's gone offline shows that in the next List() instead of its last-known-good
// status forever.
func (s Server) WithHealthResult(status ServerStatus, checkedAt time.Time, errMessage *string) Server {
	s.LastStatus = status
	s.LastCheckedAt = &checkedAt
	s.LastError = errMessage
	return s
}
