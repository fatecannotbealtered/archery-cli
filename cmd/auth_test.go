package cmd

import (
	"strings"
	"testing"
)

func TestAuthLoginFlags(t *testing.T) {
	cmd := authLoginCmd

	tests := []struct {
		name      string
		flagName  string
		wantType  string // "string"
		wantValue string // default value
	}{
		{"url", "url", "string", ""},
		{"username", "username", "string", ""},
		{"password", "password", "string", ""},
		{"region", "region", "string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered on auth login", tt.flagName)
				return
			}
			if f.Value.Type() != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.flagName, f.Value.Type(), tt.wantType)
			}

			if tt.wantType == "string" {
				got, err := cmd.Flags().GetString(tt.flagName)
				if err != nil {
					t.Fatalf("GetString(%q): %v", tt.flagName, err)
				}
				if got != tt.wantValue {
					t.Errorf("flag %q default = %q, want %q", tt.flagName, got, tt.wantValue)
				}
			}
		})
	}
}

func TestValidateAuthURL(t *testing.T) {
	origExit := lastExit
	t.Cleanup(func() { lastExit = origExit })

	valid := []string{
		"https://archery.example.com",
		"https://archery.example.com/base",
		"http://localhost:9123",
		"http://127.0.0.1:9123",
		"http://[::1]:9123",
	}
	for _, url := range valid {
		t.Run("valid "+url, func(t *testing.T) {
			lastExit = 0
			if err := validateAuthURL(url); err != nil {
				t.Fatalf("validateAuthURL(%q) returned error: %v", url, err)
			}
		})
	}

	invalid := []string{
		"",
		"archery.example.com",
		"ftp://archery.example.com",
		"http://archery.example.com",
		"http://10.0.0.10",
	}
	for _, url := range invalid {
		t.Run("invalid "+url, func(t *testing.T) {
			lastExit = 0
			if err := validateAuthURL(url); err == nil {
				t.Fatalf("validateAuthURL(%q) returned nil, want error", url)
			}
			if lastExit != ExitBadArgs {
				t.Fatalf("lastExit = %d, want %d", lastExit, ExitBadArgs)
			}
		})
	}
}

func TestAuthLogoutFlags(t *testing.T) {
	// auth logout has no command-specific flags;
	// the --region flag is a persistent flag on rootCmd.
	f := rootCmd.PersistentFlags().Lookup("region")
	if f == nil {
		t.Fatal("persistent flag 'region' not registered on rootCmd")
		return
	}
	if f.Value.Type() != "string" {
		t.Errorf("flag 'region' type = %q, want 'string'", f.Value.Type())
	}
	got, err := rootCmd.PersistentFlags().GetString("region")
	if err != nil {
		t.Fatalf("GetString('region'): %v", err)
	}
	if got != "" {
		t.Errorf("flag 'region' default = %q, want ''", got)
	}
}

func TestAuthStatusFlags(t *testing.T) {
	// auth status has no command-specific flags;
	// the --region flag is a persistent flag on rootCmd.
	f := rootCmd.PersistentFlags().Lookup("region")
	if f == nil {
		t.Fatal("persistent flag 'region' not registered on rootCmd")
		return
	}
	if f.Value.Type() != "string" {
		t.Errorf("flag 'region' type = %q, want 'string'", f.Value.Type())
	}
	got, err := rootCmd.PersistentFlags().GetString("region")
	if err != nil {
		t.Fatalf("GetString('region'): %v", err)
	}
	if got != "" {
		t.Errorf("flag 'region' default = %q, want ''", got)
	}
}

// TestAuthLogin_TOTPSecretFlagRegistered pins the flag onto the command so a
// refactor cannot quietly drop the unattended 2FA path.
func TestAuthLogin_TOTPSecretFlagRegistered(t *testing.T) {
	f := authLoginCmd.Flags().Lookup("totp-secret")
	if f == nil {
		t.Fatal("auth login must expose --totp-secret")
	}
	if f.DefValue != "" {
		t.Errorf("--totp-secret default = %q, want empty", f.DefValue)
	}
	// The help must steer operators to the env channel: argv is visible in
	// process listings and shell history (SEC-SPEC §4).
	if !strings.Contains(f.Usage, "ARCHERY_CLI_2FA_SECRET") {
		t.Errorf("--totp-secret help should point at the env var, got %q", f.Usage)
	}
}

// TestEffectiveTOTPSecretForLogin_PrecedenceAndIsolation covers the two rules
// that make the login path predictable: the flag beats the env, and neither
// consults the keyring — auth login is what WRITES the secret, so reading a
// store it is about to overwrite would make the result depend on leftovers.
func TestEffectiveTOTPSecretForLogin_PrecedenceAndIsolation(t *testing.T) {
	origFlag := authLoginTOTPFlag
	t.Cleanup(func() { authLoginTOTPFlag = origFlag })

	t.Setenv("ARCHERY_CLI_2FA_SECRET", "ENVSECRETENVSECRETENVSECRET")
	authLoginTOTPFlag = ""
	if got := effectiveTOTPSecretForLogin(); got != "ENVSECRETENVSECRETENVSECRET" {
		t.Errorf("with no flag, want the env value, got %q", got)
	}

	authLoginTOTPFlag = "FLAGSECRETFLAGSECRETFLAGSECRET"
	if got := effectiveTOTPSecretForLogin(); got != "FLAGSECRETFLAGSECRETFLAGSECRET" {
		t.Errorf("the flag must win over the env, got %q", got)
	}

	authLoginTOTPFlag = "   "
	if got := effectiveTOTPSecretForLogin(); got != "ENVSECRETENVSECRETENVSECRET" {
		t.Errorf("a blank flag must fall through to the env, got %q", got)
	}
}
