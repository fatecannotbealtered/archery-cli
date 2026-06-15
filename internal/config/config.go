package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// jsonMarshalIndent marshals config JSON; overridden in tests. // test hook
var jsonMarshalIndent = json.MarshalIndent

// Transport modes. Session is the default:普通账号即可用，走 Archery 的 AJAX
// 端点 + Django 会话 cookie。JWT 保留为可选高级模式，走 /api REST。
const (
	// ModeSession authenticates with a Django session cookie (default).
	ModeSession = "session"
	// ModeJWT authenticates with a REST Bearer token.
	ModeJWT = "jwt"
)

// RegionConfig stores Archery authentication information for a single region.
// Password is never persisted; tokens and session cookies live only in the OS keyring.
type RegionConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	// Mode selects the transport: "session" (default) or "jwt".
	Mode         string `json:"mode,omitempty"`
	Password     string `json:"password,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenExpiry  string `json:"token_expiry,omitempty"`
	// SessionID / CSRFToken are the cached Django session cookies (session mode).
	// Persisted only in the OS keyring, never on disk.
	SessionID string `json:"session_id,omitempty"`
	CSRFToken string `json:"csrf_token,omitempty"`
}

// EffectiveMode returns the region's transport mode, defaulting to session.
func (r RegionConfig) EffectiveMode() string {
	if strings.EqualFold(strings.TrimSpace(r.Mode), ModeJWT) {
		return ModeJWT
	}
	return ModeSession
}

// Config stores Archery configuration with support for multiple regions.
type Config struct {
	DefaultRegion   string                  `json:"default_region"`
	CredentialStore string                  `json:"credentialStore,omitempty"`
	Regions         map[string]RegionConfig `json:"regions"`
}

// Dir returns the configuration directory path ~/.archery-cli/
func Dir() string {
	// Prefer HOME when set (tests and cross-platform overrides); fall back to OS user home.
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".archery-cli")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".archery-cli"
	}
	return filepath.Join(home, ".archery-cli")
}

// FilePath returns the configuration file path ~/.archery-cli/config.json
func FilePath() string {
	return filepath.Join(Dir(), "config.json")
}

// firstNonEmpty returns the first non-empty trimmed string from candidates.
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if v := strings.TrimSpace(c); v != "" {
			return v
		}
	}
	return ""
}

// ActiveRegion returns the name of the active region, determined by:
//
//	ARCHERY_CLI_REGION env var (highest precedence)
//	Config.DefaultRegion
//
// Returns empty string if neither is set.
func ActiveRegion(cfg *Config) string {
	if v := firstNonEmpty(os.Getenv("ARCHERY_CLI_REGION")); v != "" {
		return v
	}
	return cfg.DefaultRegion
}

// Load reads the configuration with env var overrides:
//
//	ARCHERY_CLI_URL      — overrides region URL
//	ARCHERY_CLI_USERNAME — overrides region username
//	ARCHERY_CLI_PASSWORD — overrides region password
//	ARCHERY_CLI_REGION   — selects active region
//
// Returns an empty Config (no error) if no source has values.
// Corrupt config JSON returns an explicit error.
func Load() (*Config, error) {
	cfg := &Config{
		Regions: make(map[string]RegionConfig),
	}

	// 1. Read config file
	data, err := os.ReadFile(FilePath())
	if err == nil {
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			return nil, fmt.Errorf("parsing config %s: %w", FilePath(), jsonErr)
		}
		if cfg.Regions == nil {
			cfg.Regions = make(map[string]RegionConfig)
		}
		stripDiskSecrets(cfg)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// 2. Env vars can override the active region's fields
	regionName := ActiveRegion(cfg)
	envURL := firstNonEmpty(os.Getenv("ARCHERY_CLI_URL"))
	envUser := firstNonEmpty(os.Getenv("ARCHERY_CLI_USERNAME"))
	envPass := firstNonEmpty(os.Getenv("ARCHERY_CLI_PASSWORD"))
	envMode := normalizeMode(os.Getenv("ARCHERY_CLI_MODE"))

	if envURL != "" || envUser != "" || envPass != "" || envMode != "" {
		if regionName == "" {
			regionName = "default"
		}
		region := cfg.Regions[regionName]
		if envURL != "" {
			region.URL = envURL
		}
		if envUser != "" {
			region.Username = envUser
		}
		if envPass != "" {
			region.Password = envPass
		}
		if envMode != "" {
			region.Mode = envMode
		}
		cfg.Regions[regionName] = region
		if cfg.DefaultRegion == "" {
			cfg.DefaultRegion = regionName
		}
	}

	// 3. Try keyring for cached secrets if the config says keyring is in use.
	// JWT tokens and session cookies are both restored here based on region mode.
	if cfg.usesKeyringStore() {
		ts := NewTokenStore()
		for name, region := range cfg.Regions {
			switch region.EffectiveMode() {
			case ModeJWT:
				if region.AccessToken == "" && region.Username != "" {
					at, rt, err := ts.LoadTokens(name, region.Username)
					if err == nil && at != "" {
						region.AccessToken = at
						region.RefreshToken = rt
						cfg.Regions[name] = region
					}
				}
			default:
				if region.SessionID == "" && region.Username != "" {
					sid, csrf, err := ts.LoadSession(name, region.Username)
					if err == nil && sid != "" {
						region.SessionID = sid
						region.CSRFToken = csrf
						cfg.Regions[name] = region
					}
				}
			}
		}
	}

	return cfg, nil
}

// normalizeMode maps a free-form mode string to a canonical mode or "".
func normalizeMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ModeJWT:
		return ModeJWT
	case ModeSession:
		return ModeSession
	default:
		return ""
	}
}

func stripDiskSecrets(cfg *Config) {
	for name, region := range cfg.Regions {
		if region.Password != "" || region.AccessToken != "" || region.RefreshToken != "" ||
			region.TokenExpiry != "" || region.SessionID != "" || region.CSRFToken != "" {
			region.Password = ""
			region.AccessToken = ""
			region.RefreshToken = ""
			region.TokenExpiry = ""
			region.SessionID = ""
			region.CSRFToken = ""
			cfg.Regions[name] = region
		}
	}
}

// Save writes the configuration to disk.
//
// JWT tokens are stored in the OS keyring (not in config.json).
// Passwords are never persisted after login.
// File permissions are set to 0600 (owner-readable only) to restrict access.
func Save(cfg *Config) error {
	ts := NewTokenStore()
	store := ts.ActiveStore()
	cfg.CredentialStore = store

	if configHasSecrets(cfg) && store != CredentialStoreKeyring {
		return errors.New("OS credential store unavailable; refusing to write credentials to config file")
	}

	for name, region := range cfg.Regions {
		if region.AccessToken != "" {
			if err := ts.SaveTokens(name, region.Username, region.AccessToken, region.RefreshToken); err != nil {
				return fmt.Errorf("saving tokens to keyring: %w", err)
			}
		}
		if region.SessionID != "" {
			if err := ts.SaveSession(name, region.Username, region.SessionID, region.CSRFToken); err != nil {
				return fmt.Errorf("saving session to keyring: %w", err)
			}
		}
	}

	disk := cfg.onDiskCopy(store)

	dir := Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := jsonMarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(FilePath(), data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func configHasSecrets(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	for _, region := range cfg.Regions {
		if strings.TrimSpace(region.AccessToken) != "" || strings.TrimSpace(region.RefreshToken) != "" ||
			strings.TrimSpace(region.SessionID) != "" || strings.TrimSpace(region.CSRFToken) != "" {
			return true
		}
	}
	return false
}

// onDiskCopy returns a config safe to write to config.json.
// Tokens are always stripped; persistent secrets live only in the OS keyring.
// Password is never written to disk; use auth login or env vars for password input.
func (c *Config) onDiskCopy(store string) *Config {
	disk := &Config{
		DefaultRegion:   c.DefaultRegion,
		CredentialStore: store,
		Regions:         make(map[string]RegionConfig, len(c.Regions)),
	}
	for name, region := range c.Regions {
		rc := RegionConfig{
			URL:      region.URL,
			Username: region.Username,
			Mode:     region.Mode,
		}
		disk.Regions[name] = rc
	}
	return disk
}

// usesKeyringStore reports whether the config was persisted with the keyring store.
func (c *Config) usesKeyringStore() bool {
	return strings.TrimSpace(c.CredentialStore) == CredentialStoreKeyring
}

// MustLoad reads the configuration and validates required fields.
func MustLoad() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	regionName := ActiveRegion(cfg)
	if regionName == "" {
		return nil, errors.New("not configured: run 'archery-cli auth login' or set ARCHERY_CLI_URL, ARCHERY_CLI_USERNAME, and ARCHERY_CLI_PASSWORD environment variables")
	}

	region, ok := cfg.Regions[regionName]
	if !ok {
		return nil, fmt.Errorf("region %q not found in config; run 'archery-cli auth login' or set ARCHERY_CLI_URL environment variable", regionName)
	}

	if region.URL == "" {
		return nil, fmt.Errorf("region %q has no URL configured; set ARCHERY_CLI_URL or run 'archery-cli auth login'", regionName)
	}

	if strings.TrimSpace(region.Username) == "" || strings.TrimSpace(region.Password) == "" {
		// Allow if we already hold a cached credential (JWT token or session cookie).
		if strings.TrimSpace(region.AccessToken) == "" && strings.TrimSpace(region.SessionID) == "" {
			return nil, fmt.Errorf("region %q has no credentials; run 'archery-cli auth login' or set ARCHERY_CLI_USERNAME and ARCHERY_CLI_PASSWORD", regionName)
		}
	}

	if !strings.HasPrefix(region.URL, "https://") && !strings.HasPrefix(region.URL, "http://") {
		return nil, errors.New("URL must start with https:// (or http:// for local development)")
	}

	// http:// is only allowed for loopback hosts (local dev).
	if strings.HasPrefix(region.URL, "http://") {
		host := strings.TrimPrefix(region.URL, "http://")
		// Strip path / port / userinfo for the host check.
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		host = strings.ToLower(host)
		if host != "localhost" && host != "127.0.0.1" && host != "[::1]" && host != "::1" {
			return nil, fmt.Errorf("http:// is only allowed for loopback hosts (localhost, 127.0.0.1, [::1]); got %q", region.URL)
		}
	}

	return cfg, nil
}

// Delete removes the configuration file and any keyring secrets.
func Delete() error {
	// Clean up keyring secrets before removing the config file.
	if disk, err := readDiskConfig(); err == nil {
		ts := NewTokenStore()
		for name, region := range disk.Regions {
			_ = ts.DeleteTokens(name, region.Username)
		}
	}
	err := os.Remove(FilePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting config: %w", err)
	}
	return nil
}

// readDiskConfig reads only what is on disk (no keyring lookup).
func readDiskConfig() (*Config, error) {
	data, err := os.ReadFile(FilePath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", FilePath(), err)
	}
	return &cfg, nil
}

// IsConfigured reports whether credentials are available for the active region.
func IsConfigured() bool {
	cfg, err := Load()
	if err != nil {
		return false
	}
	regionName := ActiveRegion(cfg)
	if regionName == "" {
		return false
	}
	region, ok := cfg.Regions[regionName]
	if !ok {
		return false
	}
	return region.URL != "" && (strings.TrimSpace(region.AccessToken) != "" ||
		strings.TrimSpace(region.SessionID) != "" ||
		(strings.TrimSpace(region.Username) != "" && strings.TrimSpace(region.Password) != ""))
}

// KeyringAvailable reports whether the OS secret store accepts read/write.
func KeyringAvailable() bool {
	return NewKeyringStore().IsAvailable()
}

// CredentialStoreLabel describes where secrets are stored for the given config.
func CredentialStoreLabel(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.usesKeyringStore() {
		return CredentialStoreKeyring
	}
	return CredentialStoreNone
}
