package cmd

import (
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
)

// TestErrorCodeForAPIErr_HonorsExplicitCode guards the per-item/session error
// taxonomy: a 2FA-required failure carries StatusCode 401 AND an explicit Code,
// and the explicit Code must win so the agent receives E_2FA_REQUIRED (exit 9,
// "supply --otp") rather than a generic E_AUTH (exit 4, "fix credentials").
func TestErrorCodeForAPIErr_HonorsExplicitCode(t *testing.T) {
	twoFA := &api.APIError{StatusCode: 401, Code: api.Code2FARequired, ErrorMessages: []string{"need otp"}}
	if got := errorCodeForAPIErr(twoFA); got != output.ErrorCode(api.Code2FARequired) {
		t.Errorf("errorCodeForAPIErr(2FA) = %q, want %q", got, api.Code2FARequired)
	}

	// No explicit Code → fall back to the status mapping (401 → E_AUTH).
	plain401 := &api.APIError{StatusCode: 401}
	if got := errorCodeForAPIErr(plain401); got != output.E_AUTH {
		t.Errorf("errorCodeForAPIErr(plain 401) = %q, want E_AUTH", got)
	}

	// A non-API error stays E_NETWORK.
	if got := errorCodeForAPIErr(errBatch("boom")); got != output.E_NETWORK {
		t.Errorf("errorCodeForAPIErr(plain err) = %q, want E_NETWORK", got)
	}
}
