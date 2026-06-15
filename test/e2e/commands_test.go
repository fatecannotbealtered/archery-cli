package e2e

import (
	"strings"
	"testing"
)

// boundaryCase drives one leaf command through the real CLI boundary against
// the universal mock upstream. Write commands run their --dry-run step and
// must return a confirm token; read commands must return an ok envelope.
type boundaryCase struct {
	name    string
	args    []string
	write   bool // append --dry-run and expect data.confirm_token
	noToken bool // write command whose dry-run executes a read-only analysis
	gated   bool // route absent in v1.8.5: command fails fast with E_NOT_FOUND (exit 3)
}

var boundaryCases = []boundaryCase{
	// archive
	{name: "archive list", args: []string{"archive", "list"}},
	{name: "archive apply", write: true, args: []string{
		"archive", "apply", "--title", "t", "--group", "g", "--src-instance", "src",
		"--src-db", "db", "--src-table", "tb", "--mode", "purge", "--condition", "id<100", "--dangerous"}},
	{name: "archive audit", write: true, args: []string{"archive", "audit", "1", "--action", "pass"}},
	{name: "archive switch", write: true, args: []string{"archive", "switch", "1", "--state", "enable"}},
	{name: "archive once", write: true, args: []string{"archive", "once", "1", "--dangerous"}},
	{name: "archive log", args: []string{"archive", "log", "1"}},

	// auth (local config writes; no upstream state)
	{name: "auth login", write: true, args: []string{
		"auth", "login", "--url", "http://127.0.0.1:9", "--username", "u", "--password", "p"}},
	{name: "auth logout", write: true, args: []string{"auth", "logout"}},
	{name: "auth status", args: []string{"auth", "status"}},

	// binlog
	{name: "binlog list", args: []string{"binlog", "list", "--instance", "inst1"}},
	{name: "binlog parse", write: true, args: []string{"binlog", "parse", "--instance", "inst1", "--dangerous"}},
	{name: "binlog purge", write: true, args: []string{
		"binlog", "purge", "--instance", "1", "--binlog", "mysql-bin.000001", "--dangerous"}},

	// lifecycle / self-description
	{name: "changelog", args: []string{"changelog"}},
	{name: "context", args: []string{"context"}},
	{name: "doctor", args: []string{"doctor"}},
	{name: "reference", args: []string{"reference"}},

	// diagnostic
	{name: "diagnostic process", args: []string{"diagnostic", "process", "--instance", "inst1"}},
	{name: "diagnostic kill", write: true, args: []string{
		"diagnostic", "kill", "--instance", "inst1", "--threads", "1,2", "--dangerous"}},
	{name: "diagnostic tablespace", args: []string{"diagnostic", "tablespace", "--instance", "inst1"}},
	{name: "diagnostic locks", args: []string{"diagnostic", "locks", "--instance", "inst1"}},
	{name: "diagnostic transactions", args: []string{"diagnostic", "transactions", "--instance", "inst1"}},

	// dict
	{name: "dict tables", args: []string{"dict", "tables", "--instance", "inst1", "--db", "db1"}},
	{name: "dict table-info", args: []string{"dict", "table-info", "--instance", "inst1", "--db", "db1", "--table", "t1"}},
	{name: "dict views", gated: true, args: []string{"dict", "views", "--instance", "inst1", "--db", "db1"}},
	{name: "dict triggers", gated: true, args: []string{"dict", "triggers", "--instance", "inst1", "--db", "db1"}},
	{name: "dict procedures", gated: true, args: []string{"dict", "procedures", "--instance", "inst1", "--db", "db1"}},
	{name: "dict export", args: []string{"dict", "export", "--instance", "inst1", "--db", "db1"}},

	// instance
	{name: "instance list", args: []string{"instance", "list"}},
	{name: "instance detail", args: []string{"instance", "detail", "1"}},
	{name: "instance resource", args: []string{"instance", "resource", "--instance", "1", "--type", "database"}},
	{name: "instance describe", args: []string{"instance", "describe", "--instance", "inst1", "--db", "db1", "--table", "t1"}},
	{name: "instance create", write: true, args: []string{
		"instance", "create", "--name", "n", "--type", "master", "--db-type", "mysql",
		"--host", "127.0.0.1", "--port", "3306", "--user", "root", "--dangerous"}},
	{name: "instance update", write: true, args: []string{"instance", "update", "1", "--host", "127.0.0.2", "--dangerous"}},
	{name: "instance delete", write: true, args: []string{"instance", "delete", "1", "--dangerous"}},
	{name: "instance table-instances", args: []string{"instance", "table-instances", "--table", "t1"}},
	{name: "instance users", args: []string{"instance", "users", "--instance", "1"}},

	// query
	{name: "query run", write: true, args: []string{"query", "run", "--instance", "inst1", "--db", "db1", "--sql", "select 1", "--dangerous"}},
	{name: "query explain", write: true, noToken: true, args: []string{"query", "explain", "--instance", "inst1", "--db", "db1", "--sql", "select 1", "--dangerous"}},
	{name: "query log", args: []string{"query", "log"}},
	{name: "query favorite", write: true, args: []string{"query", "favorite", "1"}},
	{name: "query generate", gated: true, args: []string{
		"query", "generate", "--instance", "inst1", "--db", "db1", "--table", "t1", "--desc", "count rows"}},

	// slowquery
	{name: "slowquery review", args: []string{
		"slowquery", "review", "--instance", "inst1", "--start", "2026-01-01 00:00:00", "--end", "2026-01-31 23:59:59"}},
	{name: "slowquery history", args: []string{
		"slowquery", "history", "--instance", "inst1", "--start", "2026-01-01 00:00:00",
		"--end", "2026-01-31 23:59:59", "--sql-id", "abc"}},
	{name: "slowquery optimize", write: true, noToken: true, args: []string{
		"slowquery", "optimize", "--instance", "inst1", "--db", "db1", "--sql", "select 1", "--dangerous"}},

	// user
	{name: "user list", args: []string{"user", "list"}},
	{name: "user groups", args: []string{"user", "groups"}},
	{name: "user resource-groups", args: []string{"user", "resource-groups"}},

	// workflow (default session transport: submit/sqlcheck key on names)
	{name: "workflow list", args: []string{"workflow", "list"}},
	{name: "workflow audit-list", args: []string{"workflow", "audit-list"}},
	{name: "workflow detail", args: []string{"workflow", "detail", "1"}},
	{name: "workflow submit", write: true, args: []string{
		"workflow", "submit", "--name", "n", "--instance-name", "inst1", "--group-name", "g1",
		"--db", "db1", "--sql", "alter table t add c int"}},
	{name: "workflow audit", write: true, args: []string{"workflow", "audit", "1", "--action", "pass"}},
	{name: "workflow execute", write: true, args: []string{"workflow", "execute", "1", "--dangerous"}},
	{name: "workflow cancel", write: true, args: []string{"workflow", "cancel", "1"}},
	{name: "workflow sqlcheck", args: []string{
		"workflow", "sqlcheck", "--instance-name", "inst1", "--db", "db1", "--sql", "select 1"}},
	{name: "workflow auto-review", write: true, noToken: true, args: []string{
		"workflow", "auto-review", "--instance-name", "inst1", "--db", "db1", "--sql", "select 1"}},
}

func TestBoundary_AllLeafCommands(t *testing.T) {
	for _, tc := range boundaryCases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			args := append([]string{"--format", "json"}, tc.args...)
			if tc.write {
				args = append(args, "--dry-run")
			}
			r := runCLI(home, nil, args...)

			// Gated commands have no route in v1.8.5 and must fail fast with an
			// E_NOT_FOUND envelope (exit 3) instead of touching the upstream.
			if tc.gated {
				if r.ExitCode != 3 {
					t.Fatalf("gated command exit = %d, want 3\nstdout: %s", r.ExitCode, r.Stdout)
				}
				env := decodeEnvelope(t, r.Stdout)
				if env.OK || env.Error == nil || env.Error.Code != "E_NOT_FOUND" {
					t.Fatalf("gated command wants E_NOT_FOUND envelope, got: %s", r.Stdout)
				}
				return
			}

			data := decodeOK(t, r)
			if tc.write && !tc.noToken {
				token, _ := data["confirm_token"].(string)
				if !strings.HasPrefix(token, "ct_") {
					t.Fatalf("dry-run should return a confirm_token, got: %s", r.Stdout)
				}
			}
		})
	}
}

// TestBoundary_DryRunConfirmCycle exercises the full two-step write loop in
// separate processes sharing one HOME (the HMAC confirm.secret must persist).
func TestBoundary_DryRunConfirmCycle(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "workflow", "audit", "7", "--action", "pass", "--dry-run")
	data := decodeOK(t, dry)
	token, _ := data["confirm_token"].(string)
	if !strings.HasPrefix(token, "ct_") {
		t.Fatalf("no confirm_token in dry-run: %s", dry.Stdout)
	}

	run := runCLI(home, nil, "--format", "json", "workflow", "audit", "7", "--action", "pass", "--confirm", token)
	decodeOK(t, run)
}

func TestBoundary_ConfirmTokenFromOtherMachineRejected(t *testing.T) {
	homeA := t.TempDir()
	dry := runCLI(homeA, nil, "--format", "json", "workflow", "audit", "7", "--action", "pass", "--dry-run")
	data := decodeOK(t, dry)
	token, _ := data["confirm_token"].(string)

	// A different HOME means a different confirm.secret: the token must die.
	homeB := t.TempDir()
	run := runCLI(homeB, nil, "--format", "json", "workflow", "audit", "7", "--action", "pass", "--confirm", token)
	if run.ExitCode != 6 {
		t.Fatalf("exit code = %d, want 6 (conflict)\nstdout: %s", run.ExitCode, run.Stdout)
	}
	env := decodeEnvelope(t, run.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_CONFLICT" {
		t.Fatalf("want E_CONFLICT envelope, got: %s", run.Stdout)
	}
}

func TestBoundary_MissingCredentialsIsAuthError(t *testing.T) {
	r := runCLI(t.TempDir(), map[string]string{
		"ARCHERY_CLI_URL": "", "ARCHERY_CLI_USERNAME": "", "ARCHERY_CLI_PASSWORD": "",
	}, "--format", "json", "workflow", "list")
	if r.ExitCode != 4 {
		t.Fatalf("exit code = %d, want 4\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil {
		t.Fatalf("want error envelope on stdout, got: %s", r.Stdout)
	}
}

func TestBoundary_MissingRequiredFlagIsUsageError(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "query", "run")
	if r.ExitCode != 2 {
		t.Fatalf("exit code = %d, want 2\nstdout: %s", r.ExitCode, r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil {
		t.Fatalf("want error envelope on stdout, got: %s", r.Stdout)
	}
}
