package middleware

import (
	"strings"
	"unicode/utf8"
)

// External hints are kept separate from the existing Ops/billing identifier.
func normalizeExternalCorrelationID(value string) (string, bool) {
	if !utf8.ValidString(value) {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxPersistentRequestIDBytes {
		return "", false
	}
	for i := range len(value) {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return "", false
	}
	return value, true
}
