package cmd

import (
	"testing"
)

func TestReadOnlyFlagRegistered(t *testing.T) {
	for _, name := range []string{"read-only", "otp"} {
		if f := rootCmd.PersistentFlags().Lookup(name); f == nil {
			t.Fatalf("persistent flag %q not registered on rootCmd", name)
		}
	}
}

func TestApplyReadOnlyFromEnv(t *testing.T) {
	defer func() { readOnlyMode = false }()

	readOnlyMode = false
	t.Setenv("ARCHERY_CLI_READONLY", "")
	applyReadOnlyFromEnv()
	if readOnlyMode {
		t.Error("readOnlyMode should stay false when env unset")
	}

	t.Setenv("ARCHERY_CLI_READONLY", "1")
	applyReadOnlyFromEnv()
	if !readOnlyMode {
		t.Error("readOnlyMode should be true when ARCHERY_CLI_READONLY is non-empty")
	}

	// The flag, once set, is never cleared by the env helper.
	readOnlyMode = true
	t.Setenv("ARCHERY_CLI_READONLY", "")
	applyReadOnlyFromEnv()
	if !readOnlyMode {
		t.Error("an explicitly-set flag must win over an empty env")
	}
}

func TestEffectiveOTP(t *testing.T) {
	defer func() { otpFlag = "" }()

	otpFlag = ""
	t.Setenv("ARCHERY_CLI_OTP", "")
	if got := effectiveOTP(); got != "" {
		t.Errorf("effectiveOTP() = %q, want empty", got)
	}

	t.Setenv("ARCHERY_CLI_OTP", "654321")
	if got := effectiveOTP(); got != "654321" {
		t.Errorf("effectiveOTP() = %q, want env value", got)
	}

	otpFlag = "123456"
	if got := effectiveOTP(); got != "123456" {
		t.Errorf("effectiveOTP() = %q, want flag value (flag wins over env)", got)
	}
}

func TestGlobalFlags(t *testing.T) {
	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	// These are all persistent flags on rootCmd.
	tests := []flagTest{
		{
			name:     "format",
			flagName: "format",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetString("format")
				if got != "json" {
					t.Errorf("default = %q, want 'json'", got)
				}
			},
		},
		{
			name:     "compact",
			flagName: "compact",
			wantType: "bool",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetBool("compact")
				if got != false {
					t.Errorf("default = %v, want false", got)
				}
			},
		},
		{
			name:     "quiet",
			flagName: "quiet",
			wantType: "bool",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetBool("quiet")
				if got != false {
					t.Errorf("default = %v, want false", got)
				}
			},
		},
		{
			name:     "dry-run",
			flagName: "dry-run",
			wantType: "bool",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetBool("dry-run")
				if got != false {
					t.Errorf("default = %v, want false", got)
				}
			},
		},
		{
			name:     "dangerous",
			flagName: "dangerous",
			wantType: "bool",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetBool("dangerous")
				if got != false {
					t.Errorf("default = %v, want false", got)
				}
			},
		},
		{
			name:     "confirm",
			flagName: "confirm",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetString("confirm")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "region",
			flagName: "region",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := rootCmd.PersistentFlags().GetString("region")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := rootCmd.PersistentFlags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("persistent flag %q not registered on rootCmd", tt.flagName)
			}
			if f.Value.Type() != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.flagName, f.Value.Type(), tt.wantType)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t)
			}
		})
	}
}

func TestFieldsFlag(t *testing.T) {
	// --fields is a per-command flag, not a persistent rootCmd flag.
	// Verify it exists on a command that registers it.
	f := workflowListCmd.Flags().Lookup("fields")
	if f == nil {
		t.Fatal("flag 'fields' not registered on workflowListCmd")
		return
	}
	if f.Value.Type() != "string" {
		t.Errorf("flag 'fields' type = %q, want 'string'", f.Value.Type())
	}
	got, _ := workflowListCmd.Flags().GetString("fields")
	if got != "" {
		t.Errorf("flag 'fields' default = %q, want ''", got)
	}
}

func TestVersionFlag(t *testing.T) {
	origExit := lastExit
	defer func() { lastExit = origExit }()
	lastExit = 0

	rootCmd.SetArgs([]string{"--version"})
	_ = rootCmd.Execute()

	// --version should exit cleanly (exit 0).
	if lastExit != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, lastExit)
	}
}
