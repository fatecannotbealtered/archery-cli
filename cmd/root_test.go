package cmd

import (
	"testing"
)

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
