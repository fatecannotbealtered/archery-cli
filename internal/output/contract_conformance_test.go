package output

import (
	"encoding/json"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/contract"
)

// allErrorCodes enumerates every ErrorCode this tool can emit. Keep in sync with
// the const block in json.go; the conformance test asserts each is part of the
// canonical fleet contract (contract/contract.json, single-sourced from the
// ai-native-cli-spec template) with the exact exit code and retryability.
var allErrorCodes = []ErrorCode{
	E_CONFIG, E_AUTH, E_FORBIDDEN, E_NOT_FOUND, E_USAGE,
	E_VALIDATION, E_CONFIRMATION_REQUIRED, E_2FA_REQUIRED,
	E_CONFLICT, E_RATE_LIMITED, E_SERVER, E_NETWORK,
	E_TIMEOUT, E_INTEGRITY, E_IO, E_INTERRUPTED, E_UNKNOWN,
}

// TestContractConformance_ErrorCodes asserts every emitted error code is in the
// canonical contract (core ∪ this tool's ext) with the exact exit + retryable.
// This is the CI-red guard against the drift the fleet audit found (misnamed
// codes, wrong exit-code mappings).
func TestContractConformance_ErrorCodes(t *testing.T) {
	for _, c := range allErrorCodes {
		spec, ok := contract.Codes[string(c)]
		if !ok {
			t.Errorf("error code %q is not in the canonical contract (core∪ext)", c)
			continue
		}
		if got := ExitCodeForErrorCode(c); got != spec.Exit {
			t.Errorf("exit drift for %q: tool=%d contract=%d", c, got, spec.Exit)
		}
		if got := RetryableErrorCode(c); got != spec.Retryable {
			t.Errorf("retryable drift for %q: tool=%v contract=%v", c, got, spec.Retryable)
		}
	}
}

func TestContractConformance_SchemaVersion(t *testing.T) {
	if SchemaVersion != contract.SchemaVersion {
		t.Fatalf("schema_version drift: output=%q contract=%q", SchemaVersion, contract.SchemaVersion)
	}
}

// TestContractConformance_EnvelopeKeys asserts the success and error envelopes
// (and meta) carry only the canonical top-level keys, catching extra/renamed
// fields (e.g. a stray meta.timestamp).
func TestContractConformance_EnvelopeKeys(t *testing.T) {
	checkSuccessEnvelopeKeys(t, NewSuccessEnvelope(map[string]any{"x": 1}), contract.SuccessEnvelopeKeys, "success")
	checkErrorEnvelopeKeys(t, NewErrorEnvelope("m", 0, E_VALIDATION), contract.ErrorEnvelopeKeys, "error")
}

func checkSuccessEnvelopeKeys(t *testing.T, env SuccessEnvelope, canonical []string, label string) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal %s envelope: %v", label, err)
	}
	checkTopAndMetaKeys(t, b, canonical, label)
}

func checkErrorEnvelopeKeys(t *testing.T, env ErrorEnvelope, canonical []string, label string) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal %s envelope: %v", label, err)
	}
	checkTopAndMetaKeys(t, b, canonical, label)
}

func checkTopAndMetaKeys(t *testing.T, b []byte, canonical []string, label string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("unmarshal %s envelope: %v", label, err)
	}
	// "data"/"error" are omitempty and may be absent; flag only UNEXPECTED keys.
	for k := range top {
		if !conformanceContains(canonical, k) && k != "data" && k != "error" {
			t.Errorf("%s envelope has unexpected top-level key %q (canonical: %v)", label, k, canonical)
		}
	}
	for _, req := range []string{"ok", "schema_version", "meta"} {
		if _, ok := top[req]; !ok {
			t.Errorf("%s envelope missing required key %q", label, req)
		}
	}
	var meta map[string]json.RawMessage
	if raw, ok := top["meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	allowed := append(append([]string{}, contract.MetaRequiredKeys...), contract.MetaOptionalKeys...)
	for k := range meta {
		if !conformanceContains(allowed, k) {
			t.Errorf("meta has unexpected key %q (canonical: %v)", k, allowed)
		}
	}
}

func conformanceContains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}
