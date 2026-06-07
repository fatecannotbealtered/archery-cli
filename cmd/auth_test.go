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
		{"username", "username", "string", ""},
		{"password", "password", "string", ""},
		{"region", "region", "string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered on auth login", tt.flagName)
			}
			if f.Value.Type() != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.flagName, f.Value.Type(), tt.wantType)
			}

			switch tt.wantType {
			case "string":
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

func TestAuthLogoutFlags(t *testing.T) {
	// auth logout has no command-specific flags;
	// the --region flag is a persistent flag on rootCmd.
	f := rootCmd.PersistentFlags().Lookup("region")
	if f == nil {
		t.Fatal("persistent flag 'region' not registered on rootCmd")
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
