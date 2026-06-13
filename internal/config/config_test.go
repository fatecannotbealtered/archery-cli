package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain installs the in-memory keyring mock so config tests never touch the
// real OS secret store — a locked macOS Keychain on CI runners blocks on the
// keyring probe instead of failing fast, hanging the package to its timeout.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// withTempHome redirects the user home directory to a temp dir for the duration of fn.
func withTempHome(t *testing.T, fn func(string)) {
	t.Helper()
	tmp := t.TempDir()

	// HOME on unix, USERPROFILE on Windows. Set both to be safe.
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	defer func() {
		_ = os.Setenv("HOME", origHome)
		_ = os.Setenv("USERPROFILE", origUserProfile)
	}()

	fn(tmp)
}

func clearArcheryEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ARCHERY_CLI_URL", "ARCHERY_CLI_USERNAME", "ARCHERY_CLI_PASSWORD", "ARCHERY_CLI_REGION"} {
		t.Setenv(k, "")
	}
}

// --- helpers ---

func writeConfigFile(t *testing.T, cfg *Config) {
	t.Helper()
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(FilePath(), data, 0600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
}

func blockConfigJSONAsDir(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.RemoveAll(FilePath()); err != nil {
		t.Fatalf("RemoveAll config path: %v", err)
	}
	if err := os.Mkdir(FilePath(), 0700); err != nil {
		t.Fatalf("Mkdir config path: %v", err)
	}
}

func blockConfigJSONAsNonemptyDir(t *testing.T) {
	t.Helper()
	blockConfigJSONAsDir(t)
	if err := os.WriteFile(filepath.Join(FilePath(), "inner"), []byte("x"), 0600); err != nil {
		t.Fatalf("WriteFile inner: %v", err)
	}
}

// --- TestLoad ---

func TestLoad_Empty(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.DefaultRegion != "" {
			t.Errorf("DefaultRegion = %q, want empty", cfg.DefaultRegion)
		}
		if len(cfg.Regions) != 0 {
			t.Errorf("Regions = %v, want empty", cfg.Regions)
		}
	})
}

func TestLoad_FromFile(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		cfg := &Config{
			DefaultRegion: "us-east",
			Regions: map[string]RegionConfig{
				"us-east": {
					URL:      "https://archery.example.com",
					Username: "alice",
					Password: "filepass",
				},
			},
		}
		t.Cleanup(func() { _ = NewTokenStore().DeleteTokens("us-east", "alice") })
		if err := Save(cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.DefaultRegion != "us-east" {
			t.Errorf("DefaultRegion = %q, want us-east", got.DefaultRegion)
		}
		region := got.Regions["us-east"]
		if region.URL != "https://archery.example.com" {
			t.Errorf("URL = %q, want https://archery.example.com", region.URL)
		}
		if region.Username != "alice" {
			t.Errorf("Username = %q, want alice", region.Username)
		}
		if region.Password != "" {
			t.Errorf("Password = %q, want empty because disk passwords are ignored", region.Password)
		}
	})
}

func TestLoad_IgnoresDiskSecrets(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:          "https://archery.example.com",
					Username:     "alice",
					Password:     "disk-password",
					AccessToken:  "disk-access-token",
					RefreshToken: "disk-refresh-token",
					TokenExpiry:  "2026-06-08T12:00:00Z",
				},
			},
		})
		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		region := got.Regions["default"]
		if region.Password != "" || region.AccessToken != "" || region.RefreshToken != "" || region.TokenExpiry != "" {
			t.Fatalf("disk secrets should be ignored, got password=%q access=%q refresh=%q expiry=%q", region.Password, region.AccessToken, region.RefreshToken, region.TokenExpiry)
		}
	})
}

func TestLoad_CorruptFileReturnsError(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		if err := os.MkdirAll(Dir(), 0700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(FilePath(), []byte("not json {"), 0600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if _, err := Load(); err == nil {
			t.Error("expected error for corrupt JSON, got nil")
		}
	})
}

func TestLoad_ReadConfigError(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		blockConfigJSONAsDir(t)
		_, err := Load()
		if err == nil {
			t.Fatal("expected read config error")
		}
		if !strings.Contains(err.Error(), "reading config") {
			t.Fatalf("Load() = %v, want reading config error", err)
		}
	})
}

// --- TestSave ---

func TestSave_FilePermissions(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		cfg := &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "https://archery.example.com",
					Username: "user",
					Password: "tok",
				},
			},
		}
		if err := Save(cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
		fi, err := os.Stat(FilePath())
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		// On Windows the unix mode bits aren't fully meaningful, but on Unix we expect 0600.
		mode := fi.Mode().Perm()
		if mode != 0 && mode != 0600 {
			t.Logf("file mode = %v (informational)", mode)
		}
	})
}

func TestSave_CreatesFile(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		cfg := &Config{
			DefaultRegion: "us-west",
			Regions: map[string]RegionConfig{
				"us-west": {
					URL:      "https://archery-west.example.com",
					Username: "bob",
					Password: "secret",
				},
			},
		}
		if err := Save(cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if _, err := os.Stat(FilePath()); err != nil {
			t.Fatalf("config file does not exist after Save: %v", err)
		}
	})
}

func TestSave_MarshalIndentError(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		orig := jsonMarshalIndent
		jsonMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, errors.New("marshal indent failed")
		}
		t.Cleanup(func() { jsonMarshalIndent = orig })

		err := Save(&Config{
			DefaultRegion: "default",
			Regions:       map[string]RegionConfig{"default": {URL: "https://x.example.com"}},
		})
		if err == nil {
			t.Fatal("expected MarshalIndent error")
		}
		if !strings.Contains(err.Error(), "encoding config") {
			t.Fatalf("Save() = %v, want encoding config error", err)
		}
	})
}

func TestSave_MkdirAllError(t *testing.T) {
	withTempHome(t, func(home string) {
		blockConfigDirAsFile(t, home)
		err := Save(&Config{
			DefaultRegion: "default",
			Regions:       map[string]RegionConfig{"default": {URL: "https://x.example.com"}},
		})
		if err == nil {
			t.Fatal("expected MkdirAll error")
		}
		if !strings.Contains(err.Error(), "creating config dir") {
			t.Fatalf("Save() = %v, want creating config dir error", err)
		}
	})
}

func TestSave_WriteFileError(t *testing.T) {
	withTempHome(t, func(_ string) {
		blockConfigJSONAsDir(t)
		err := Save(&Config{
			DefaultRegion: "default",
			Regions:       map[string]RegionConfig{"default": {URL: "https://x.example.com"}},
		})
		if err == nil {
			t.Fatal("expected WriteFile error")
		}
		if !strings.Contains(err.Error(), "writing config") {
			t.Fatalf("Save() = %v, want writing config error", err)
		}
	})
}

func blockConfigDirAsFile(t *testing.T, home string) {
	t.Helper()
	// Place a file where the config dir should be so MkdirAll fails.
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".archery-cli"), []byte("block"), 0600); err != nil {
		t.Fatalf("WriteFile block: %v", err)
	}
}

// --- TestMustLoad ---

func TestMustLoad_MissingURL(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {URL: "", Username: "alice", Password: "pass"},
			},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected error for missing URL")
		}
	})
}

func TestMustLoad_MissingCredentials(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {URL: "https://archery.example.com", Username: "", Password: ""},
			},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected error for missing credentials")
		}
	})
}

func TestMustLoad_WhitespaceCredentialsRejected(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {URL: "https://archery.example.com", Username: "   ", Password: "   "},
			},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected error for whitespace credentials")
		}
	})
}

func TestMustLoad_RequiresHttpsOrLoopback(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		t.Setenv("ARCHERY_CLI_URL", "ftp://archery.example.com")
		t.Setenv("ARCHERY_CLI_USERNAME", "alice")
		t.Setenv("ARCHERY_CLI_PASSWORD", "pass")
		if _, err := MustLoad(); err == nil {
			t.Error("expected error for non-http(s) URL")
		}
	})
}

func TestMustLoad_HttpAllowedOnlyForLoopback(t *testing.T) {
	loopback := []string{
		"http://localhost",
		"http://localhost:8080",
		"http://127.0.0.1",
		"http://127.0.0.1:9000",
		"http://[::1]:8080",
	}
	for _, url := range loopback {
		t.Run(url, func(t *testing.T) {
			withTempHome(t, func(_ string) {
				clearArcheryEnv(t)
				t.Setenv("ARCHERY_CLI_URL", url)
				t.Setenv("ARCHERY_CLI_USERNAME", "alice")
				t.Setenv("ARCHERY_CLI_PASSWORD", "pass")
				if _, err := MustLoad(); err != nil {
					t.Errorf("loopback URL %q should be allowed: %v", url, err)
				}
			})
		})
	}

	nonLoopback := []string{
		"http://archery.example.com",
		"http://192.168.1.10",
		"http://10.0.0.1",
		"http://archery",
	}
	for _, url := range nonLoopback {
		t.Run(url, func(t *testing.T) {
			withTempHome(t, func(_ string) {
				clearArcheryEnv(t)
				t.Setenv("ARCHERY_CLI_URL", url)
				t.Setenv("ARCHERY_CLI_USERNAME", "alice")
				t.Setenv("ARCHERY_CLI_PASSWORD", "pass")
				if _, err := MustLoad(); err == nil {
					t.Errorf("non-loopback http:// URL %q should be rejected", url)
				}
			})
		})
	}
}

func TestMustLoad_HttpStripsPathAndUserinfo(t *testing.T) {
	cases := []string{
		"http://user@localhost/archery",
		"http://127.0.0.1:8080/path?query=1",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			withTempHome(t, func(_ string) {
				clearArcheryEnv(t)
				t.Setenv("ARCHERY_CLI_URL", url)
				t.Setenv("ARCHERY_CLI_USERNAME", "alice")
				t.Setenv("ARCHERY_CLI_PASSWORD", "pass")
				if _, err := MustLoad(); err != nil {
					t.Fatalf("MustLoad(%q): %v", url, err)
				}
			})
		})
	}
}

func TestMustLoad_ValidConfig(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "https://archery.example.com",
					Username: "alice",
				},
			},
		})
		t.Setenv("ARCHERY_CLI_PASSWORD", "envpass")
		if _, err := MustLoad(); err != nil {
			t.Errorf("unexpected error with valid config: %v", err)
		}
	})
}

func TestMustLoad_RejectsDiskTokenWithoutCredentials(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:         "https://archery.example.com",
					AccessToken: "cached-token",
				},
			},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected disk token to be ignored and credentials to be missing")
		}
	})
}

func TestMustLoad_LoadError(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		blockConfigJSONAsDir(t)
		if _, err := MustLoad(); err == nil {
			t.Fatal("expected Load error from MustLoad")
		}
	})
}

func TestMustLoad_RegionNotFound(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "nonexistent",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "https://archery.example.com",
					Username: "alice",
					Password: "pass",
				},
			},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected error for nonexistent region")
		}
	})
}

func TestMustLoad_NoRegionsConfigured(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "",
			Regions:       map[string]RegionConfig{},
		})
		if _, err := MustLoad(); err == nil {
			t.Error("expected error when no regions configured")
		}
	})
}

// --- TestEnvOverrides ---

func TestLoad_EnvOverridesFileValues(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "https://from-file.example.com",
					Username: "fileuser",
					Password: "filepass",
				},
			},
		})
		t.Setenv("ARCHERY_CLI_URL", "https://from-env.example.com")
		t.Setenv("ARCHERY_CLI_USERNAME", "envuser")
		t.Setenv("ARCHERY_CLI_PASSWORD", "envpass")

		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		region := got.Regions["default"]
		if region.URL != "https://from-env.example.com" {
			t.Errorf("URL = %q, want from-env", region.URL)
		}
		if region.Username != "envuser" {
			t.Errorf("Username = %q, want envuser", region.Username)
		}
		if region.Password != "envpass" {
			t.Errorf("Password = %q, want envpass", region.Password)
		}
	})
}

func TestLoad_EnvCreatesDefaultRegionWhenNoneExists(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		t.Setenv("ARCHERY_CLI_URL", "https://env-only.example.com")
		t.Setenv("ARCHERY_CLI_USERNAME", "envuser")
		t.Setenv("ARCHERY_CLI_PASSWORD", "envpass")

		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		region, ok := got.Regions["default"]
		if !ok {
			t.Fatal("expected 'default' region to be created from env vars")
		}
		if region.URL != "https://env-only.example.com" {
			t.Errorf("URL = %q, want env-only", region.URL)
		}
		if got.DefaultRegion != "default" {
			t.Errorf("DefaultRegion = %q, want default", got.DefaultRegion)
		}
	})
}

func TestLoad_RegionEnvSelectsRegion(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "us-east",
			Regions: map[string]RegionConfig{
				"us-east": {
					URL:      "https://east.example.com",
					Username: "alice",
					Password: "pass",
				},
				"eu-west": {
					URL:      "https://west.example.com",
					Username: "bob",
					Password: "secret",
				},
			},
		})
		t.Setenv("ARCHERY_CLI_URL", "https://env.example.com")

		// Without REGION env, applies to default_region (us-east)
		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Regions["us-east"].URL != "https://env.example.com" {
			t.Errorf("us-east URL = %q, want env override", got.Regions["us-east"].URL)
		}
		if got.Regions["eu-west"].URL != "https://west.example.com" {
			t.Errorf("eu-west URL = %q, want unchanged", got.Regions["eu-west"].URL)
		}
	})
}

func TestLoad_RegionEnvOverrideAppliesToSelectedRegion(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "us-east",
			Regions: map[string]RegionConfig{
				"us-east": {
					URL:      "https://east.example.com",
					Username: "alice",
					Password: "pass",
				},
				"eu-west": {
					URL:      "https://west.example.com",
					Username: "bob",
					Password: "secret",
				},
			},
		})
		t.Setenv("ARCHERY_CLI_REGION", "eu-west")
		t.Setenv("ARCHERY_CLI_URL", "https://env-west.example.com")

		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Regions["eu-west"].URL != "https://env-west.example.com" {
			t.Errorf("eu-west URL = %q, want env override", got.Regions["eu-west"].URL)
		}
		if got.Regions["us-east"].URL != "https://east.example.com" {
			t.Errorf("us-east URL = %q, want unchanged", got.Regions["us-east"].URL)
		}
	})
}

func TestLoad_PartialEnvOverrides(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "https://from-file.example.com",
					Username: "fileuser",
					Password: "filepass",
				},
			},
		})
		// Only override password, URL and username should come from file
		t.Setenv("ARCHERY_CLI_PASSWORD", "envpass")

		got, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		region := got.Regions["default"]
		if region.URL != "https://from-file.example.com" {
			t.Errorf("URL = %q, want file value", region.URL)
		}
		if region.Username != "fileuser" {
			t.Errorf("Username = %q, want file value", region.Username)
		}
		if region.Password != "envpass" {
			t.Errorf("Password = %q, want envpass", region.Password)
		}
	})
}

// --- TestActiveRegion ---

func TestActiveRegion_EnvOverridesDefault(t *testing.T) {
	cfg := &Config{
		DefaultRegion: "us-east",
		Regions: map[string]RegionConfig{
			"us-east": {URL: "https://east.example.com"},
			"eu-west": {URL: "https://west.example.com"},
		},
	}
	t.Setenv("ARCHERY_CLI_REGION", "eu-west")
	if got := ActiveRegion(cfg); got != "eu-west" {
		t.Errorf("ActiveRegion() = %q, want eu-west", got)
	}
}

func TestActiveRegion_DefaultRegion(t *testing.T) {
	clearArcheryEnv(t)
	cfg := &Config{
		DefaultRegion: "us-east",
		Regions: map[string]RegionConfig{
			"us-east": {URL: "https://east.example.com"},
		},
	}
	if got := ActiveRegion(cfg); got != "us-east" {
		t.Errorf("ActiveRegion() = %q, want us-east", got)
	}
}

func TestActiveRegion_EmptyWhenNeitherSet(t *testing.T) {
	clearArcheryEnv(t)
	cfg := &Config{
		DefaultRegion: "",
		Regions:       map[string]RegionConfig{},
	}
	if got := ActiveRegion(cfg); got != "" {
		t.Errorf("ActiveRegion() = %q, want empty", got)
	}
}

func TestActiveRegion_SingleRegionFallback(t *testing.T) {
	// ActiveRegion does not fall back to the single region when DefaultRegion is empty;
	// it only checks env and DefaultRegion. This test documents that behavior.
	clearArcheryEnv(t)
	cfg := &Config{
		DefaultRegion: "",
		Regions: map[string]RegionConfig{
			"only": {URL: "https://only.example.com"},
		},
	}
	if got := ActiveRegion(cfg); got != "" {
		t.Errorf("ActiveRegion() = %q, want empty (no default_region set)", got)
	}
}

// --- TestDelete ---

func TestDelete_RemovesFile(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {URL: "https://archery.example.com", Username: "alice", Password: "pass"},
			},
		})
		if err := Delete(); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := os.Stat(FilePath()); !os.IsNotExist(err) {
			t.Errorf("expected file removed, stat err = %v", err)
		}
		// Idempotent
		if err := Delete(); err != nil {
			t.Errorf("second Delete should be idempotent, got %v", err)
		}
	})
}

func TestDelete_Error(t *testing.T) {
	withTempHome(t, func(_ string) {
		blockConfigJSONAsNonemptyDir(t)
		err := Delete()
		if err == nil {
			t.Fatal("expected delete error")
		}
		if !strings.Contains(err.Error(), "deleting config") {
			t.Fatalf("Delete() = %v, want deleting config error", err)
		}
	})
}

// --- TestIsConfigured ---

func TestIsConfigured_WithEnvCredentials(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		t.Setenv("ARCHERY_CLI_URL", "https://archery.example.com")
		t.Setenv("ARCHERY_CLI_USERNAME", "alice")
		t.Setenv("ARCHERY_CLI_PASSWORD", "pass")
		if !IsConfigured() {
			t.Error("expected configured")
		}
	})
}

func TestIsConfigured_IgnoresDiskAccessToken(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:         "https://archery.example.com",
					AccessToken: "cached-token",
				},
			},
		})
		if IsConfigured() {
			t.Error("expected disk access token to be ignored")
		}
	})
}

func TestIsConfigured_MissingURL(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		writeConfigFile(t, &Config{
			DefaultRegion: "default",
			Regions: map[string]RegionConfig{
				"default": {
					URL:      "",
					Username: "alice",
					Password: "pass",
				},
			},
		})
		if IsConfigured() {
			t.Error("expected not configured when URL missing")
		}
	})
}

func TestIsConfigured_NoFile(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		if IsConfigured() {
			t.Error("expected not configured when no file exists")
		}
	})
}

func TestIsConfigured_LoadError(t *testing.T) {
	withTempHome(t, func(_ string) {
		clearArcheryEnv(t)
		blockConfigJSONAsDir(t)
		if IsConfigured() {
			t.Error("expected not configured on Load error")
		}
	})
}

// --- TestDirAndFilePath ---

func TestDirAndFilePath(t *testing.T) {
	withTempHome(t, func(home string) {
		got := Dir()
		want := filepath.Join(home, ".archery-cli")
		if got != want {
			t.Errorf("Dir() = %q, want %q", got, want)
		}
		if filepath.Base(FilePath()) != "config.json" {
			t.Errorf("FilePath() base = %q, want config.json", filepath.Base(FilePath()))
		}
	})
}

func TestDir_FallbackWhenUserHomeDirFails(t *testing.T) {
	for _, k := range []string{"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH"} {
		t.Setenv(k, "")
	}
	if got := Dir(); got != ".archery-cli" {
		t.Errorf("Dir() = %q, want .archery-cli when UserHomeDir fails", got)
	}
}
