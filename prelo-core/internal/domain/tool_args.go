package domain

import (
	"encoding/json"
	"fmt"
	"sort"
)

// toolSchema is the subset of JSON Schema the curated tools use: an object with typed top-level
// properties, required keys and (optionally) no extra keys.
type toolSchema struct {
	Properties map[string]struct {
		Type string   `json:"type"`
		Enum []string `json:"enum"`
	} `json:"properties"`
	Required             []string `json:"required"`
	AdditionalProperties *bool    `json:"additionalProperties"`
}

// ValidateToolArgs checks a model's arguments against the tool's schema before the tool runs —
// a malformed call becomes a tool result the model can correct, never a half-executed action.
func ValidateToolArgs(def ToolDefinition, argsJSON string) error {
	if argsJSON == "" {
		argsJSON = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return &ValidationError{Message: "arguments must be a JSON object"}
	}
	if len(def.InputSchema) == 0 {
		return nil
	}
	var schema toolSchema
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		return fmt.Errorf("tool %s has an invalid schema: %w", def.Name, err)
	}
	for _, key := range schema.Required {
		if _, ok := args[key]; !ok {
			return &ValidationError{Message: "missing required argument: " + key}
		}
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		prop, known := schema.Properties[key]
		if !known {
			if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
				return &ValidationError{Message: "unknown argument: " + key}
			}
			continue
		}
		if !matchesType(prop.Type, args[key]) {
			return &ValidationError{Message: fmt.Sprintf("argument %s must be of type %s", key, prop.Type)}
		}
		if len(prop.Enum) > 0 {
			value, _ := args[key].(string)
			ok := false
			for _, allowed := range prop.Enum {
				ok = ok || allowed == value
			}
			if !ok {
				return &ValidationError{Message: fmt.Sprintf("argument %s must be one of %v", key, prop.Enum)}
			}
		}
	}
	return nil
}

func matchesType(kind string, value any) bool {
	switch kind {
	case "", "any":
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		f, ok := value.(float64)
		return ok && f == float64(int64(f))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	}
	return false
}
