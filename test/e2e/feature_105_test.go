package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestReadOnly_BlocksWritesViaFlag confirms --read-only refuses a write command
// at the chokepoint with an E_FORBIDDEN envelope (exit 4), even under --dry-run.
func TestReadOnly_BlocksWritesViaFlag(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "--read-only", "--dry-run",
		"workflow", "submit", "--name", "n", "--instance-name", "inst1",
		"--group-name", "g1", "--db", "db1", "--sql", "select 1")
	if r.ExitCode != 4 {
		t.Fatalf("exit = %d, want 4\nstdout: %s", r.ExitCode, r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_FORBIDDEN" {
		t.Fatalf("want E_FORBIDDEN envelope, got: %s", r.Stdout)
	}
	if !strings.Contains(env.Error.Message, "read-only") {
		t.Fatalf("message should mention read-only, got: %q", env.Error.Message)
	}
}

// TestReadOnly_BlocksWritesViaEnv confirms ARCHERY_CLI_READONLY has the same
// effect as the flag.
func TestReadOnly_BlocksWritesViaEnv(t *testing.T) {
	r := runCLI(t.TempDir(), map[string]string{"ARCHERY_CLI_READONLY": "1"},
		"--format", "json", "--dry-run",
		"workflow", "audit", "1", "--action", "pass")
	if r.ExitCode != 4 {
		t.Fatalf("exit = %d, want 4\nstdout: %s", r.ExitCode, r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_FORBIDDEN" {
		t.Fatalf("want E_FORBIDDEN envelope, got: %s", r.Stdout)
	}
}

// TestReadOnly_AllowsReads confirms read commands are unaffected by read-only.
func TestReadOnly_AllowsReads(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "--read-only", "workflow", "list")
	if r.ExitCode != 0 {
		t.Fatalf("read command under --read-only exit = %d, want 0\nstdout: %s\nstderr: %s",
			r.ExitCode, r.Stdout, r.Stderr)
	}
	decodeOK(t, r)
}

// TestReadOnly_ReflectedInContext confirms context reports readOnly=true.
func TestReadOnly_ReflectedInContext(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "--read-only", "context")
	env := decodeEnvelope(t, r.Stdout)
	var ctx struct {
		ReadOnly bool `json:"readOnly"`
	}
	if err := json.Unmarshal(env.Data, &ctx); err != nil {
		t.Fatalf("decode context: %v\n%s", err, r.Stdout)
	}
	if !ctx.ReadOnly {
		t.Fatalf("context.readOnly = false, want true\n%s", r.Stdout)
	}
}

// TestWorkflowDetail_SurfacesExecutionResult confirms `workflow detail` now
// exposes the execution result rows (result[]) and the string status code,
// the data reworked for 1.0.5 so an agent can see why an execution failed.
func TestWorkflowDetail_SurfacesExecutionResult(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "workflow", "detail", "1")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	env := decodeEnvelope(t, r.Stdout)
	var detail struct {
		StatusCode string `json:"statusCode"`
		Result     []struct {
			Stage       string `json:"stage"`
			StageStatus string `json:"stageStatus"`
			ErrLevel    int    `json:"errLevel"`
			Error       string `json:"error"`
		} `json:"result"`
	}
	if err := json.Unmarshal(env.Data, &detail); err != nil {
		t.Fatalf("decode detail: %v\n%s", err, r.Stdout)
	}
	if detail.StatusCode != "workflow_exception" {
		t.Fatalf("statusCode = %q, want workflow_exception\n%s", detail.StatusCode, r.Stdout)
	}
	if len(detail.Result) == 0 {
		t.Fatalf("result[] empty; want the execution row\n%s", r.Stdout)
	}
	if detail.Result[0].Error != "Invalid remote backup information" {
		t.Fatalf("result[0].error = %q, want the backup error\n%s", detail.Result[0].Error, r.Stdout)
	}
}

// TestWorkflowSubmit_ResolvesInstanceAndGroupID confirms session-mode submit
// accepts the numeric --instance / --group (resolved to names via the list
// endpoints), the parameter unification added in 1.0.5. The dry-run preview
// must carry the resolved names, not the raw IDs.
func TestWorkflowSubmit_ResolvesInstanceAndGroupID(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "--dry-run",
		"workflow", "submit", "--name", "n", "--instance", "1", "--group", "1",
		"--db", "db1", "--sql", "alter table t add c int")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	data := decodeOK(t, r)
	preview, _ := data["preview"].(map[string]any)
	changes, _ := preview["changes"].([]any)
	if len(changes) == 0 {
		t.Fatalf("no preview changes\n%s", r.Stdout)
	}
	change, _ := changes[0].(map[string]any)
	if change["instanceName"] != "inst1" {
		t.Fatalf("instanceName = %v, want inst1 (resolved from --instance 1)\n%s", change["instanceName"], r.Stdout)
	}
	if change["groupName"] != "ops" {
		t.Fatalf("groupName = %v, want ops (resolved from --group 1)\n%s", change["groupName"], r.Stdout)
	}
}

// TestWorkflowSubmit_UnknownInstanceIDNotFound confirms an unresolvable
// --instance ID fails with a clear E_NOT_FOUND (exit 3).
func TestWorkflowSubmit_UnknownInstanceIDNotFound(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "json", "--dry-run",
		"workflow", "submit", "--name", "n", "--instance", "999", "--group", "1",
		"--db", "db1", "--sql", "select 1")
	if r.ExitCode != 3 {
		t.Fatalf("exit = %d, want 3\nstdout: %s", r.ExitCode, r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_NOT_FOUND" {
		t.Fatalf("want E_NOT_FOUND envelope, got: %s", r.Stdout)
	}
}
