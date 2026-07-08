package cmd

import "testing"

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
