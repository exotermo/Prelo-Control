package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

func TestActionMessagesSayExactlyWhatIsAuthorized(t *testing.T) {
	payload := json.RawMessage(`{"repository":"exotermo/loja","commitSha":"0123456789abcdef0123456789abcdef01234567","environment":"production","target":"loja.example.com","app":"loja","deployRequestId":"d1"}`)
	action := domain.ActionRequest{ID: uuid.New(), Kind: "deploy", Payload: payload, Risk: domain.RiskHigh,
		Impact: "Publica a loja", RequestedBy: "github:exotermo/loja@deploy.yml"}
	approval := domain.NewActionApproval(action.ID, ActionScope(action), 30*time.Minute)
	msg := ActionApprovalMessage(ActionView{Action: action, Approval: approval})
	for _, want := range []string{approval.ShortCode, "risco ALTO", "exotermo/loja", "0123456789abcdef0123456789abcdef01234567",
		"production → loja.example.com", "Publica a loja", "só para este commit", "SIM " + approval.ShortCode} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	url := "https://loja.example.com"
	action.Result = &domain.ActionResult{Status: "SUCCEEDED", Sequence: 2, URL: &url}
	if got := ActionResultMessage(ActionView{Action: action, Approval: approval}); !strings.Contains(got, "concluído") || !strings.Contains(got, url) {
		t.Fatalf("result message: %s", got)
	}
}
