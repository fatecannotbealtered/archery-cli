package cmd

import (
	"testing"
	"time"
)

// isolateHome points os.UserHomeDir at a fresh temp dir so the consumed-token
// store never touches (or leaks into) the real ~/.archery-cli during tests.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// os.UserHomeDir consults HOME on unix and USERPROFILE on Windows.
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestConfirmTokenSingleUse(t *testing.T) {
	isolateHome(t)
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	freezeTime(t, now)

	action := "instance.delete"
	region := "cn-east-1"
	payload := map[string]any{"id": 42}

	token, _ := newConfirmToken(action, region, payload)

	// First confirm: dry-run already produced the token; this is the real write.
	confirmFlag = token
	if err := requireConfirm(nil, action, region, payload); err != nil {
		t.Fatalf("first confirm should succeed, got %v", err)
	}

	// Replaying the same token must be rejected with E_CONFLICT (exit 6).
	lastExit = 0
	t.Cleanup(func() { lastExit = 0 })
	confirmFlag = token
	err := requireConfirm(nil, action, region, payload)
	if err == nil {
		t.Fatal("replay of consumed token should fail, got nil")
	}
	if lastExit != ExitConflict {
		t.Errorf("replay exit code = %d, want %d (ExitConflict)", lastExit, ExitConflict)
	}
}

func TestTokenFingerprintStable(t *testing.T) {
	token := "ct_1749384000_deadbeefcafef00d"

	first := tokenFingerprint(token)
	second := tokenFingerprint(token)
	if first != second {
		t.Errorf("fingerprint not stable: %q vs %q", first, second)
	}
	if len(first) != 16 {
		t.Errorf("fingerprint length = %d, want 16", len(first))
	}
	if other := tokenFingerprint(token + "x"); other == first {
		t.Errorf("distinct tokens share a fingerprint: %q", other)
	}
}

func TestConfirmTokenExpiryUnix(t *testing.T) {
	if got := confirmTokenExpiryUnix("ct_1749384000_deadbeef"); got != 1749384000 {
		t.Errorf("expiry = %d, want 1749384000", got)
	}
	if got := confirmTokenExpiryUnix("not-a-token"); got != 0 {
		t.Errorf("malformed token expiry = %d, want 0", got)
	}
}
