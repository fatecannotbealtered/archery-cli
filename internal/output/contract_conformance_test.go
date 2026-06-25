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
	// Assert "data" is present in a success envelope built with a non-empty payload.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err == nil {
		if _, ok := top["data"]; !ok {
			t.Errorf("%s envelope missing \"data\" key (success envelopes with non-empty payload must include data)", label)
		}
	}
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
	// Assert every MetaRequiredKey is present.
	for _, req := range contract.MetaRequiredKeys {
		if _, ok := meta[req]; !ok {
			t.Errorf("%s envelope meta missing required key %q", label, req)
		}
	}
	allowed := append(append([]string{}, contract.MetaRequiredKeys...), contract.MetaOptionalKeys...)
	for k := range meta {
		if !conformanceContains(allowed, k) {
			t.Errorf("meta has unexpected key %q (canonical: %v)", k, allowed)
		}
	}
}

// TestContractConformance_ErrorCodeTable is an independent hardcoded assertion of
// the canonical 16-code table (exit + retryable). It catches a wrong contract.json
// that the contract-delegating assertions cannot detect.
func TestContractConformance_ErrorCodeTable(t *testing.T) {
	type spec struct {
		exit      int
		retryable bool
	}
	// Canonical table from the fleet spec: ai-native-cli-spec/template/common/contract/contract.json.
	// E_USAGE/E_VALIDATION=2; E_NOT_FOUND=3; E_AUTH/E_FORBIDDEN/E_CONFIG=4;
	// E_CONFIRMATION_REQUIRED=5; E_CONFLICT=6;
	// E_NETWORK/E_RATE_LIMITED/E_SERVER=7 (retryable);
	// E_TIMEOUT=8 (retryable); E_INTEGRITY/E_IO/E_UNKNOWN=1; E_INTERRUPTED=130 (retryable).
	table := map[ErrorCode]spec{
		E_USAGE:                 {2, false},
		E_VALIDATION:            {2, false},
		E_NOT_FOUND:             {3, false},
		E_AUTH:                  {4, false},
		E_FORBIDDEN:             {4, false},
		E_CONFIG:                {4, false},
		E_CONFIRMATION_REQUIRED: {5, false},
		E_CONFLICT:              {6, false},
		E_NETWORK:               {7, true},
		E_RATE_LIMITED:          {7, true},
		E_SERVER:                {7, true},
		E_TIMEOUT:               {8, true},
		E_INTEGRITY:             {1, false},
		E_IO:                    {1, false},
		E_UNKNOWN:               {1, false},
		E_INTERRUPTED:           {130, true},
	}
	for code, want := range table {
		if got := ExitCodeForErrorCode(code); got != want.exit {
			t.Errorf("ExitCodeForErrorCode(%s) = %d, want %d", code, got, want.exit)
		}
		if got := RetryableErrorCode(code); got != want.retryable {
			t.Errorf("RetryableErrorCode(%s) = %v, want %v", code, got, want.retryable)
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
