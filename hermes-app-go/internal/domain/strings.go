package domain

import "strings"

func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}
