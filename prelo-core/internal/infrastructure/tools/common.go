package tools

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/exotermo/prelo-core/internal/domain"
)

// maxToolOutput keeps a tool result small enough for any model's context; longer results are cut.
const maxToolOutput = 8000

// taskReader is what a tool needs to learn which task (and so which project/client) it serves.
type taskReader interface {
	FindByID(ctx context.Context, id domain.TaskID) (domain.Task, error)
}

func decodeArgs(argsJSON string, dst any) error {
	if argsJSON == "" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), dst); err != nil {
		return &domain.ValidationError{Message: "argumentos inválidos: " + err.Error()}
	}
	return nil
}

// jsonOutput renders a tool result as compact JSON, cut to maxToolOutput.
func jsonOutput(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return truncate(string(b), maxToolOutput), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…(cortado)"
}
