package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// The contract's test vector (docs/integracoes/action-requests.md) — BastionDeploy reproduces it too.
const contractPayload = `{"deployRequestId":"d1","app":"loja","environment":"production","target":"loja.example.com","repository":"exotermo/loja","commitSha":"0123456789abcdef0123456789abcdef01234567"}`
const contractHash = "sha256:10c22d67a404931356d344a6c56a7161e8a4b156269b1eae19331fcdc7773a6a"

func TestPayloadHashMatchesTheContractVector(t *testing.T) {
	got, err := PayloadHash([]byte(contractPayload))
	if err != nil || got != contractHash {
		t.Fatalf("hash = %s %v", got, err)
	}
	canonical, _ := CanonicalJSON([]byte(" {\"b\": \"á<&>\", \"a\": {\"z\": 1, \"y\": [true, null]}} "))
	if string(canonical) != `{"a":{"y":[true,null],"z":1},"b":"á<&>"}` {
		t.Fatalf("canonical = %s", canonical)
	}
}

func TestNewActionRequestValidates(t *testing.T) {
	ws, project := uuid.New(), NewProjectID()
	ok, err := NewActionRequest(ws, project, "deploy", []byte(contractPayload), contractHash, "Publica a loja", "github:exotermo/loja", "k1", nil)
	if err != nil || ok.Risk != RiskHigh || ok.DeployOf().CommitSha[:7] != "0123456" {
		t.Fatalf("valid request refused: %v", err)
	}
	if _, err := NewActionRequest(ws, project, "deploy", []byte(contractPayload), "sha256:"+contractHash[7:70]+"0", "x", "y", "k", nil); !errors.Is(err, ErrPayloadHashMismatch) {
		t.Fatalf("wrong hash must be refused, got %v", err)
	}
	branch := `{"deployRequestId":"d1","app":"loja","environment":"production","target":"t","repository":"exotermo/loja","commitSha":"main"}`
	h, _ := PayloadHash([]byte(branch))
	if _, err := NewActionRequest(ws, project, "deploy", []byte(branch), h, "x", "y", "k", nil); err == nil {
		t.Fatal("a branch name is never a commit SHA")
	}
	if _, err := NewActionRequest(ws, project, "shell", []byte(contractPayload), contractHash, "x", "y", "k", nil); err == nil {
		t.Fatal("unknown kind must be refused")
	}
}
