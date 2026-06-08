package config

import (
	"testing"
)

func TestCredentialKey(t *testing.T) {
	tests := []struct {
		name     string
		region   string
		username string
		want     string
	}{
		{"basic", "prod", "alice", "prod|jwt|alice"},
		{"empty username uses default", "prod", "", "prod|jwt|default"},
		{"whitespace username uses default", "prod", "   ", "prod|jwt|default"},
		{"trims whitespace", " prod ", " alice ", "prod|jwt|alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := credentialKey(tt.region, tt.username)
			if got != tt.want {
				t.Errorf("credentialKey(%q, %q) = %q, want %q", tt.region, tt.username, got, tt.want)
			}
		})
	}
}

func TestNewKeyringStore(t *testing.T) {
	ks := NewKeyringStore()
	if ks.Service != keyringService {
		t.Errorf("Service = %q, want %q", ks.Service, keyringService)
	}
}

func TestNewTokenStore(t *testing.T) {
	ts := NewTokenStore()
	if ts == nil {
		t.Fatal("NewTokenStore returned nil")
	}
	if ts.keyring == nil {
		t.Fatal("keyring field is nil")
	}
}

func TestTokenStore_ActiveStore(t *testing.T) {
	ts := NewTokenStore()
	store := ts.ActiveStore()
	// Should return either "keyring" or "none" depending on environment.
	if store != CredentialStoreKeyring && store != CredentialStoreNone {
		t.Errorf("ActiveStore() = %q, want %q or %q", store, CredentialStoreKeyring, CredentialStoreNone)
	}
}

func TestKeyringAvailable(t *testing.T) {
	// Just verify it doesn't panic; result depends on environment.
	_ = KeyringAvailable()
}

func TestCredentialStoreLabel(t *testing.T) {
	if got := CredentialStoreLabel(nil); got != "" {
		t.Errorf("CredentialStoreLabel(nil) = %q, want empty", got)
	}

	cfg := &Config{CredentialStore: CredentialStoreKeyring}
	if got := CredentialStoreLabel(cfg); got != CredentialStoreKeyring {
		t.Errorf("CredentialStoreLabel(keyring) = %q, want %q", got, CredentialStoreKeyring)
	}

	cfg = &Config{CredentialStore: ""}
	if got := CredentialStoreLabel(cfg); got != CredentialStoreNone {
		t.Errorf("CredentialStoreLabel(empty) = %q, want %q", got, CredentialStoreNone)
	}
}

func TestTokenStore_SaveLoadDelete_RoundTrip(t *testing.T) {
	ts := NewTokenStore()
	if !ts.keyring.IsAvailable() {
		t.Skip("OS keyring not available in this environment")
	}

	region := "test-region"
	user := "test-user"
	access := "test-access-token"
	refresh := "test-refresh-token"

	// Clean up any leftover probe.
	_ = ts.DeleteTokens(region, user)

	if err := ts.SaveTokens(region, user, access, refresh); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	gotAccess, gotRefresh, err := ts.LoadTokens(region, user)
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if gotAccess != access {
		t.Errorf("AccessToken = %q, want %q", gotAccess, access)
	}
	if gotRefresh != refresh {
		t.Errorf("RefreshToken = %q, want %q", gotRefresh, refresh)
	}

	// Delete
	if err := ts.DeleteTokens(region, user); err != nil {
		t.Fatalf("DeleteTokens: %v", err)
	}

	gotAccess, gotRefresh, err = ts.LoadTokens(region, user)
	if err != nil {
		t.Fatalf("LoadTokens after delete: %v", err)
	}
	if gotAccess != "" || gotRefresh != "" {
		t.Errorf("expected empty tokens after delete, got access=%q refresh=%q", gotAccess, gotRefresh)
	}
}

func TestOnDiskCopy_StripsTokensWhenKeyring(t *testing.T) {
	cfg := &Config{
		DefaultRegion:   "prod",
		CredentialStore: CredentialStoreKeyring,
		Regions: map[string]RegionConfig{
			"prod": {
				URL:          "https://archery.example.com",
				Username:     "alice",
				Password:     "pass",
				AccessToken:  "at",
				RefreshToken: "rt",
			},
		},
	}

	disk := cfg.onDiskCopy(CredentialStoreKeyring)

	region := disk.Regions["prod"]
	if region.AccessToken != "" {
		t.Errorf("AccessToken should be stripped on disk with keyring, got %q", region.AccessToken)
	}
	if region.RefreshToken != "" {
		t.Errorf("RefreshToken should be stripped on disk with keyring, got %q", region.RefreshToken)
	}
	if region.Password != "" {
		t.Errorf("Password should be stripped on disk, got %q", region.Password)
	}
	if disk.CredentialStore != CredentialStoreKeyring {
		t.Errorf("CredentialStore = %q, want %q", disk.CredentialStore, CredentialStoreKeyring)
	}
}

func TestOnDiskCopy_StripsTokensWhenNoStore(t *testing.T) {
	cfg := &Config{
		DefaultRegion: "prod",
		Regions: map[string]RegionConfig{
			"prod": {
				URL:          "https://archery.example.com",
				Username:     "alice",
				Password:     "pass",
				AccessToken:  "at",
				RefreshToken: "rt",
			},
		},
	}

	disk := cfg.onDiskCopy(CredentialStoreNone)

	region := disk.Regions["prod"]
	if region.AccessToken != "" {
		t.Errorf("AccessToken should be stripped on disk, got %q", region.AccessToken)
	}
	if region.RefreshToken != "" {
		t.Errorf("RefreshToken should be stripped on disk, got %q", region.RefreshToken)
	}
	if region.Password != "" {
		t.Errorf("Password should be stripped on disk, got %q", region.Password)
	}
	if disk.CredentialStore != CredentialStoreNone {
		t.Errorf("CredentialStore = %q, want %q", disk.CredentialStore, CredentialStoreNone)
	}
}

func TestUsesKeyringStore(t *testing.T) {
	cfg := &Config{CredentialStore: CredentialStoreKeyring}
	if !cfg.usesKeyringStore() {
		t.Error("expected usesKeyringStore() = true for keyring store")
	}

	cfg = &Config{CredentialStore: CredentialStoreNone}
	if cfg.usesKeyringStore() {
		t.Error("expected usesKeyringStore() = false for no credential store")
	}

	cfg = &Config{CredentialStore: ""}
	if cfg.usesKeyringStore() {
		t.Error("expected usesKeyringStore() = false for empty store")
	}
}
