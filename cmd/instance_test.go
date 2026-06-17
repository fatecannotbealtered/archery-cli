package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/spf13/cobra"
)

// TestListInstancesSession_ForbiddenFallsBackToUserInstances reproduces the
// non-DBA case: /instance/list/ is gated by sql.menu_instance_list and returns
// 403, so the CLI must fall back to the ungated, user-scoped
// /group/user_all_instances/ (the same endpoint the web query page uses). That
// endpoint returns id as a bare NUMBER and only {id,type,db_type,instance_name},
// has no server-side search, and the fallback must map + client-filter correctly.
func TestListInstancesSession_ForbiddenFallsBackToUserInstances(t *testing.T) {
	var sawList, sawUserAll bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/instance/list/":
			sawList = true
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"msg":"permission denied"}`))
		case "/group/user_all_instances/":
			sawUserAll = true
			if r.URL.Query().Get("db_type[]") != "redis" {
				t.Errorf("db_type[] = %q, want redis", r.URL.Query().Get("db_type[]"))
			}
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":[` +
				`{"id":7,"type":"redis","db_type":"redis","instance_name":"pangu_test_redis"},` +
				`{"id":8,"type":"redis","db_type":"redis","instance_name":"other_cache"}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
		}
	}))
	defer srv.Close()

	c := api.NewClient(srv.URL)
	c.SetMode(api.ModeSession)
	c.InjectSessionCookies("sid-1", "csrf-1") // mark session ready, skip form login

	instances, total, err := listInstancesSession(c, "", "redis", "pangu", 20, 0)
	if err != nil {
		t.Fatalf("listInstancesSession: %v", err)
	}
	if !sawList || !sawUserAll {
		t.Fatalf("expected both endpoints hit: list=%v userAll=%v", sawList, sawUserAll)
	}
	// "pangu" search filters out other_cache client-side.
	if total != 1 || len(instances) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(instances))
	}
	got := instances[0]
	if got.ID != "7" {
		t.Errorf("ID = %q, want \"7\" (numeric id coerced to string)", got.ID)
	}
	if got.InstanceName != "pangu_test_redis" || got.DbType != "redis" {
		t.Errorf("instance = %+v, want pangu_test_redis/redis", got)
	}
	if !strings.Contains(got.InstanceName, "pangu") {
		t.Errorf("search filter failed: %q", got.InstanceName)
	}
}

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
