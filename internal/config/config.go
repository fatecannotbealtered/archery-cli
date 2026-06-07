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

// RegionConfig stores Archery authentication information for a single region.
type RegionConfig struct {
	URL          string `json:"url"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenExpiry  string `json:"token_expiry,omitempty"`
}

// Config stores Archery configuration with support for multiple regions.
type Config struct {
	DefaultRegion string                   `json:"default_region"`
	Regions       map[string]RegionConfig  `json:"regions"`
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// 2. Env vars can override the active region's fields
	regionName := ActiveRegion(cfg)
	envURL := firstNonEmpty(os.Getenv("ARCHERY_CLI_URL"))
	envUser := firstNonEmpty(os.Getenv("ARCHERY_CLI_USERNAME"))
	envPass := firstNonEmpty(os.Getenv("ARCHERY_CLI_PASSWORD"))

	if envURL != "" || envUser != "" || envPass != "" {
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
		cfg.Regions[regionName] = region
		if cfg.DefaultRegion == "" {
			cfg.DefaultRegion = regionName
		}
	}

	return cfg, nil
}

// Save writes the configuration to disk.
//
// SECURITY: Passwords and tokens are stored in plaintext in ~/.archery-cli/config.json.
// File permissions are set to 0600 (owner-readable only) to restrict access.
// For production use, consider integrating the OS keyring (e.g. Windows Credential Manager,
// macOS Keychain, or Linux Secret Service) to encrypt credentials at rest.
//
// TODO: Replace plaintext storage with OS keyring integration for T1 credential-at-rest compliance.
func Save(cfg *Config) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := jsonMarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(FilePath(), data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
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
		// Allow if we have a cached token (already authenticated)
		if strings.TrimSpace(region.AccessToken) == "" {
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

// Delete removes the configuration file.
func Delete() error {
	err := os.Remove(FilePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting config: %w", err)
	}
	return nil
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
		(strings.TrimSpace(region.Username) != "" && strings.TrimSpace(region.Password) != ""))
}
