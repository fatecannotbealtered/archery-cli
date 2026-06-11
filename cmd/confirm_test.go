package cmd

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freezeTime replaces confirmNow with a fixed clock and returns a restore func.
func freezeTime(t *testing.T, now time.Time) {
	t.Helper()
	original := confirmNow
	t.Cleanup(func() { confirmNow = original })
	confirmNow = func() time.Time { return now }
}

func TestNewConfirmToken(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	freezeTime(t, now)

	action := "instance.delete"
	region := "cn-east-1"
	payload := map[string]any{"id": 42}

	token, expires := newConfirmToken(action, region, payload)

	// Expires should be now + 15min, truncated to second.
	wantExpires := now.UTC().Add(confirmTokenTTL).Truncate(time.Second)
	if !expires.Equal(wantExpires) {
		t.Errorf("expires = %v, want %v", expires, wantExpires)
	}

	// Token format: ct_<unix>_<hex16>
	parts := strings.Split(token, "_")
	if len(parts) != 3 || parts[0] != "ct" {
		t.Fatalf("token format = %q, want ct_<unix>_<hex>", token)
	}

	gotUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("cannot parse unix from token: %v", err)
	}
	if gotUnix != wantExpires.Unix() {
		t.Errorf("token unix = %d, want %d", gotUnix, wantExpires.Unix())
	}

	// Verify the hash portion matches the canonical seed.
	seed := canonicalConfirmSeed(action, region, payload, wantExpires.Unix())
	sum := confirmDigest32(seed)
	wantHash := hex.EncodeToString(sum[:8])
	if parts[2] != wantHash {
		t.Errorf("token hash = %s, want %s", parts[2], wantHash)
	}
}

func TestValidateConfirmToken(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	freezeTime(t, now)

	action := "instance.delete"
	region := "cn-east-1"
	payload := map[string]any{"id": 42}

	token, _ := newConfirmToken(action, region, payload)

	t.Run("valid token passes", func(t *testing.T) {
		err := validateConfirmToken(token, action, region, payload)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("expired token fails", func(t *testing.T) {
		// Advance time past expiry.
		freezeTime(t, now.Add(confirmTokenTTL+time.Second))
		err := validateConfirmToken(token, action, region, payload)
		if err == nil {
			t.Error("expected error for expired token, got nil")
		}
	})

	t.Run("wrong token fails", func(t *testing.T) {
		wrong := "ct_" + strconv.FormatInt(now.Add(5*time.Minute).Unix(), 10) + "_deadbeefdeadbeef"
		err := validateConfirmToken(wrong, action, region, payload)
		if err == nil {
			t.Error("expected error for wrong token, got nil")
		}
	})

	t.Run("empty token fails", func(t *testing.T) {
		err := validateConfirmToken("", action, region, payload)
		if err == nil {
			t.Error("expected error for empty token, got nil")
		}
	})

	t.Run("malformed token fails", func(t *testing.T) {
		err := validateConfirmToken("not-a-valid-token", action, region, payload)
		if err == nil {
			t.Error("expected error for malformed token, got nil")
		}
	})
}

func TestConfirmTokenRegionBinding(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	freezeTime(t, now)

	action := "instance.delete"
	payload := map[string]any{"id": 42}

	tokenA, _ := newConfirmToken(action, "cn-east-1", payload)

	t.Run("token for region A fails in region B", func(t *testing.T) {
		err := validateConfirmToken(tokenA, action, "us-west-2", payload)
		if err == nil {
			t.Error("expected error when validating token in different region, got nil")
		}
	})

	t.Run("token for region A passes in region A", func(t *testing.T) {
		err := validateConfirmToken(tokenA, action, "cn-east-1", payload)
		if err != nil {
			t.Errorf("expected nil error for same region, got %v", err)
		}
	})
}

func TestConfirmTokenExpiry(t *testing.T) {
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	freezeTime(t, base)

	action := "instance.delete"
	region := "cn-east-1"
	payload := map[string]any{"id": 42}

	token, expires := newConfirmToken(action, region, payload)

	t.Run("valid 1 second before expiry", func(t *testing.T) {
		freezeTime(t, expires.Add(-time.Second))
		err := validateConfirmToken(token, action, region, payload)
		if err != nil {
			t.Errorf("expected nil 1s before expiry, got %v", err)
		}
	})

	t.Run("expired at exact expiry time", func(t *testing.T) {
		freezeTime(t, expires)
		err := validateConfirmToken(token, action, region, payload)
		if err == nil {
			t.Error("expected error at exact expiry time, got nil")
		}
	})

	t.Run("expired 1 second after expiry", func(t *testing.T) {
		freezeTime(t, expires.Add(time.Second))
		err := validateConfirmToken(token, action, region, payload)
		if err == nil {
			t.Error("expected error 1s after expiry, got nil")
		}
	})

	t.Run("TTL is 15 minutes", func(t *testing.T) {
		wantTTL := 15 * time.Minute
		gotTTL := expires.Sub(base.UTC().Truncate(time.Second))
		if gotTTL != wantTTL {
			t.Errorf("TTL = %v, want %v", gotTTL, wantTTL)
		}
	})
}

func TestCanonicalConfirmSeed(t *testing.T) {
	action := "instance.delete"
	region := "cn-east-1"
	expiresUnix := int64(1749384000)
	payload := map[string]any{"id": 42}

	seed := canonicalConfirmSeed(action, region, payload, expiresUnix)

	// Seed must contain each component.
	s := string(seed)
	if !strings.Contains(s, action) {
		t.Errorf("seed missing action %q", action)
	}
	if !strings.Contains(s, region) {
		t.Errorf("seed missing region %q", region)
	}
	if !strings.Contains(s, strconv.FormatInt(expiresUnix, 10)) {
		t.Errorf("seed missing expires unix %d", expiresUnix)
	}
	if !strings.Contains(s, `"id"`) || !strings.Contains(s, `42`) {
		t.Errorf("seed missing payload fields, got %q", s)
	}

	// Verify the separator is newline between components.
	lines := strings.Split(s, "\n")
	if len(lines) != 4 {
		t.Errorf("seed has %d lines, want 4 (action, region, expires, payload)", len(lines))
	}
	if lines[0] != action {
		t.Errorf("line[0] = %q, want %q", lines[0], action)
	}
	if lines[1] != region {
		t.Errorf("line[1] = %q, want %q", lines[1], region)
	}
	if lines[2] != strconv.FormatInt(expiresUnix, 10) {
		t.Errorf("line[2] = %q, want %q", lines[2], strconv.FormatInt(expiresUnix, 10))
	}

	t.Run("different actions produce different seeds", func(t *testing.T) {
		seed2 := canonicalConfirmSeed("instance.restart", region, payload, expiresUnix)
		if string(seed) == string(seed2) {
			t.Error("seeds should differ for different actions")
		}
	})

	t.Run("different regions produce different seeds", func(t *testing.T) {
		seed2 := canonicalConfirmSeed(action, "us-west-2", payload, expiresUnix)
		if string(seed) == string(seed2) {
			t.Error("seeds should differ for different regions")
		}
	})

	t.Run("different expiries produce different seeds", func(t *testing.T) {
		seed2 := canonicalConfirmSeed(action, region, payload, expiresUnix+1)
		if string(seed) == string(seed2) {
			t.Error("seeds should differ for different expiries")
		}
	})

	t.Run("different payloads produce different seeds", func(t *testing.T) {
		seed2 := canonicalConfirmSeed(action, region, map[string]any{"id": 99}, expiresUnix)
		if string(seed) == string(seed2) {
			t.Error("seeds should differ for different payloads")
		}
	})
}
