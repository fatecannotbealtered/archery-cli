package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestInstanceListFlags(t *testing.T) {
	cmd := instanceListCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "type",
			flagName: "type",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("type")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db-type",
			flagName: "db-type",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db-type")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "search",
			flagName: "search",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("search")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "limit",
			flagName: "limit",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("limit")
				if got != 20 {
					t.Errorf("default = %d, want 20", got)
				}
			},
		},
		{
			name:     "offset",
			flagName: "offset",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("offset")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
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

// TestInstanceOpsCommandsRegistered checks the new ops commands and their
// required flags are wired up, and that the write ones carry the dangerous gate.
func TestInstanceOpsCommandsRegistered(t *testing.T) {
	cases := []struct {
		cmd       *cobra.Command
		use       string
		flags     []string
		write     bool
		dangerous bool
	}{
		{cmd: instanceTestCmd, use: "test-instance", flags: []string{"instance"}},
		{cmd: instanceCreateDBCmd, use: "create-db", flags: []string{"instance", "db", "owner", "remark"}, write: true, dangerous: true},
		{cmd: instanceCreateUserCmd, use: "create-user", flags: []string{"instance", "user", "host", "password", "remark"}, write: true, dangerous: true},
		{cmd: instanceGrantCmd, use: "grant", flags: []string{"instance", "user-host", "op", "level", "privs", "db", "table", "column"}, write: true, dangerous: true},
	}
	for _, tc := range cases {
		t.Run(tc.use, func(t *testing.T) {
			if tc.cmd.Use != tc.use {
				t.Fatalf("use = %q, want %q", tc.cmd.Use, tc.use)
			}
			for _, name := range tc.flags {
				if tc.cmd.Flags().Lookup(name) == nil {
					t.Errorf("flag %q not registered on %q", name, tc.use)
				}
			}
			if tc.write && tc.cmd.Annotations["write"] != "true" {
				t.Errorf("%q should be marked write", tc.use)
			}
			if tc.dangerous && tc.cmd.Annotations["riskLevel"] != "high" {
				t.Errorf("%q should be high risk", tc.use)
			}
		})
	}
}

// TestGrantTypeMappings pins the op/level → Archery int mappings so a future
// refactor can't silently swap GRANT for REVOKE or db-level for table-level.
func TestGrantTypeMappings(t *testing.T) {
	if op, _ := grantOpType("grant"); op != 0 {
		t.Errorf("grant op_type = %d, want 0", op)
	}
	if op, _ := grantOpType("revoke"); op != 1 {
		t.Errorf("revoke op_type = %d, want 1", op)
	}
	if _, err := grantOpType("bogus"); err == nil {
		t.Error("bogus op should error")
	}
	want := map[string]struct {
		pt  int
		key string
	}{
		"global": {0, "global_privs"},
		"db":     {1, "db_privs"},
		"table":  {2, "tb_privs"},
		"column": {3, "col_privs"},
	}
	for level, exp := range want {
		pt, key, err := grantPrivType(level)
		if err != nil {
			t.Errorf("%s: unexpected error %v", level, err)
			continue
		}
		if pt != exp.pt || key != exp.key {
			t.Errorf("%s: got (%d,%q), want (%d,%q)", level, pt, key, exp.pt, exp.key)
		}
	}
	if _, _, err := grantPrivType("bogus"); err == nil {
		t.Error("bogus level should error")
	}
}

// TestQuoteUserHost pins the user@host → `user`@`host` normalization. The grant
// view splices user_host straight into GRANT ... TO <x>, so an unquoted value
// like user@% is a MySQL syntax error (verified live against v1.8.5).
func TestQuoteUserHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"e2e_probe@%", "`e2e_probe`@`%`"},
		{"app@10.0.0.1", "`app`@`10.0.0.1`"},
		{"app", "`app`@`%`"},                         // host defaults to %
		{"app@", "`app`@`%`"},                        // empty host defaults to %
		{"  app @ localhost ", "`app`@`localhost`"},  // trimmed
		{"`root`@`localhost`", "`root`@`localhost`"}, // pre-quoted passes through
	}
	for _, c := range cases {
		got, err := quoteUserHost(c.in)
		if err != nil {
			t.Errorf("quoteUserHost(%q) unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("quoteUserHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "@host", "@"} {
		if _, err := quoteUserHost(bad); err == nil {
			t.Errorf("quoteUserHost(%q) should error", bad)
		}
	}
}
