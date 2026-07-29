package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersAllowOnlyRequiredGoogleContactsOrigins(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	(&API{}).setSecurityHeaders(response)

	policy := response.Header().Get("Content-Security-Policy")
	for _, required := range []string{
		"script-src 'self' https://accounts.google.com/gsi/client",
		"connect-src 'self' https://accounts.google.com/gsi/ https://people.googleapis.com",
		"frame-src https://accounts.google.com/gsi/",
	} {
		if !strings.Contains(policy, required) {
			t.Fatalf("Content-Security-Policy %q does not contain %q", policy, required)
		}
	}
	if got := response.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin-allow-popups" {
		t.Fatalf("Cross-Origin-Opener-Policy = %q", got)
	}
}
