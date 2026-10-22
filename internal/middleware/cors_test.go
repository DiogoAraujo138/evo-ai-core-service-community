package middleware

import (
	"strings"
	"testing"
)

func TestCORSAllowsCompanyHeader(t *testing.T) {
	headers := CORSConfig("").AllowedHeaders
	for _, want := range []string{"X-Evo-Tenant-Id", "Authorization", "Content-Type"} {
		if !containsHeader(headers, want) {
			t.Fatalf("preflight does not allow %s; got %s", want, strings.Join(headers, ", "))
		}
	}
}

func containsHeader(headers []string, want string) bool {
	for _, header := range headers {
		if strings.EqualFold(header, want) {
			return true
		}
	}
	return false
}
