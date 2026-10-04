package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ActionRequest (PR-3, contratos G5–G8): an external system asks the Prelo to authorize one exact
// action. Approving it authorizes that payload (bound by PayloadHash) and nothing else — another
// commit, environment or target is another request. See docs/integracoes/action-requests.md.
type ActionRequest struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	ProjectID      ProjectID
	Kind           string
	Payload        json.RawMessage
	PayloadHash    string
	Risk           RiskLevel
	Impact         string
	RequestedBy    string
	IdempotencyKey string
	ApiKeyID       *uuid.UUID
	CreatedAt      time.Time
	Result         *ActionResult
}

type ActionResult struct {
	Status     string
	Sequence   int
	Message    *string
	URL        *string
	Digest     *string
	ReportedAt time.Time
}

const ActionKindDeploy = "deploy"

// ActionStartGrace: the executor must start (first result) at most this long after the approval's
// deadline — an approval that sat unused for hours is not a permit for later.
const ActionStartGrace = 10 * time.Minute

var ActionResultStatuses = map[string]bool{"RUNNING": true, "SUCCEEDED": true, "FAILED": true, "ROLLED_BACK": true, "CANCELLED": true}

var (
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// DeployPayload is the payload of kind "deploy" — every field required.
type DeployPayload struct {
	Repository      string `json:"repository"`
	CommitSha       string `json:"commitSha"`
	Environment     string `json:"environment"`
	Target          string `json:"target"`
	App             string `json:"app"`
	DeployRequestID string `json:"deployRequestId"`
}

func (p DeployPayload) validate() error {
	switch {
	case !repoPattern.MatchString(p.Repository):
		return &ValidationError{Message: "payload.repository must be owner/name"}
	case !shaPattern.MatchString(p.CommitSha):
		return &ValidationError{Message: "payload.commitSha must be the full 40-character lowercase commit SHA (never a branch)"}
	case strings.TrimSpace(p.Environment) == "", strings.TrimSpace(p.Target) == "", strings.TrimSpace(p.App) == "", strings.TrimSpace(p.DeployRequestID) == "":
		return &ValidationError{Message: "payload.environment, target, app and deployRequestId are required"}
	}
	return nil
}

// CanonicalJSON renders a JSON value with object keys sorted at every level, no insignificant
// whitespace, UTF-8 kept as is and no HTML escaping — the form both sides hash.
func CanonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonical(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case string:
		return writeString(buf, t)
	case json.Number:
		buf.WriteString(t.String())
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		return fmt.Errorf("unsupported JSON value %T", v)
	}
	return nil
}

func writeString(buf *bytes.Buffer, s string) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
	return nil
}

// PayloadHash is "sha256:" + hex(SHA-256(canonical payload)).
func PayloadHash(raw []byte) (string, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return "", &ValidationError{Message: "payload must be valid JSON"}
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// NewActionRequest validates an incoming request: known kind, well-formed payload, and a
// payloadHash that matches the server's own computation. Deploys are always HIGH risk.
func NewActionRequest(workspaceID uuid.UUID, projectID ProjectID, kind string, payload json.RawMessage, payloadHash, impact, requestedBy, idempotencyKey string, apiKeyID *uuid.UUID) (ActionRequest, error) {
	if kind != ActionKindDeploy {
		return ActionRequest{}, &ValidationError{Message: "kind must be deploy"}
	}
	var deploy DeployPayload
	if err := json.Unmarshal(payload, &deploy); err != nil {
		return ActionRequest{}, &ValidationError{Message: "payload must be a JSON object"}
	}
	if err := deploy.validate(); err != nil {
		return ActionRequest{}, err
	}
	if !hashPattern.MatchString(payloadHash) {
		return ActionRequest{}, &ValidationError{Message: "payloadHash must be sha256:<64 hex>"}
	}
	expected, err := PayloadHash(payload)
	if err != nil {
		return ActionRequest{}, err
	}
	if expected != payloadHash {
		return ActionRequest{}, ErrPayloadHashMismatch
	}
	impact = strings.TrimSpace(impact)
	if impact == "" || len([]rune(impact)) > 300 {
		return ActionRequest{}, &ValidationError{Message: "impact is required (up to 300 characters)"}
	}
	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" || len(requestedBy) > 200 {
		return ActionRequest{}, &ValidationError{Message: "requestedBy is required (up to 200 characters)"}
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return ActionRequest{}, &ValidationError{Message: "idempotencyKey is required (up to 200 characters)"}
	}
	canonical, _ := CanonicalJSON(payload)
	return ActionRequest{
		ID: uuid.New(), WorkspaceID: workspaceID, ProjectID: projectID, Kind: kind, Payload: canonical,
		PayloadHash: payloadHash, Risk: RiskHigh, Impact: impact, RequestedBy: requestedBy,
		IdempotencyKey: idempotencyKey, ApiKeyID: apiKeyID, CreatedAt: time.Now().UTC(),
	}, nil
}

// ErrPayloadHashMismatch: the caller's hash differs from the server's — the two sides disagree on
// what is being authorized, so nothing is created.
var ErrPayloadHashMismatch = &ValidationError{Message: "payloadHash does not match the canonical payload"}

// DeployOf decodes the deploy payload (for messages and listings).
func (a ActionRequest) DeployOf() DeployPayload {
	var p DeployPayload
	_ = json.Unmarshal(a.Payload, &p)
	return p
}
