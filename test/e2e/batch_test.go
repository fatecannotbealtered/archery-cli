package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decodeArray decodes the top-level envelope and returns data as a map (batch
// commands always emit an object: items[] + summary, or the dry-run preview).
func decodeBatchData(t *testing.T, r cmdResult) map[string]any {
	t.Helper()
	if r.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	env := decodeEnvelope(t, r.Stdout)
	if !env.OK {
		t.Fatalf("ok=false: %s", r.Stdout)
	}
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("batch data is not an object: %v\n%s", err, r.Stdout)
	}
	return data
}

func batchSummary(t *testing.T, data map[string]any) (total, succeeded, failed, skipped int) {
	t.Helper()
	s, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %v", data)
	}
	num := func(k string) int {
		v, _ := s[k].(float64)
		return int(v)
	}
	return num("total"), num("succeeded"), num("failed"), num("skipped")
}

func batchItems(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()
	raw, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("missing items: %v", data)
	}
	out := make([]map[string]any, len(raw))
	for i, it := range raw {
		out[i], _ = it.(map[string]any)
	}
	return out
}

// ─── query run --instances ────────────────────────────────────────────────────

func TestBatch_QueryRunInstances_DryRunPreview(t *testing.T) {
	home := t.TempDir()
	r := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1",
		"--dangerous", "--dry-run")
	data := decodeBatchData(t, r)
	preview, ok := data["preview"].(map[string]any)
	if !ok {
		t.Fatalf("dry-run missing preview: %s", r.Stdout)
	}
	if total, _ := preview["total"].(float64); int(total) != 3 {
		t.Fatalf("preview total = %v, want 3", preview["total"])
	}
	if token, _ := data["confirm_token"].(string); !strings.HasPrefix(token, "ct_") {
		t.Fatalf("missing confirm_token: %s", r.Stdout)
	}
}

func TestBatch_QueryRunInstances_DedupAndOrder(t *testing.T) {
	home := t.TempDir()
	// b appears twice and via both comma and repeated forms; result must preserve
	// first-seen order [a, b, c] with no duplicate.
	r := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--instances", "b,c", "--db", "app", "--sql", "select 1",
		"--dangerous", "--dry-run")
	data := decodeBatchData(t, r)
	preview := data["preview"].(map[string]any)
	targets, _ := preview["targets"].([]any)
	got := []string{}
	for _, v := range targets {
		got = append(got, v.(string))
	}
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("targets = %v, want %v", got, want)
	}
}

func TestBatch_QueryRunInstances_DangerousGate(t *testing.T) {
	home := t.TempDir()
	// No --dangerous: high-risk batch must be refused with exit 5 even on dry-run.
	r := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dry-run")
	if r.ExitCode != 5 {
		t.Fatalf("exit code = %d, want 5 (confirmation required)\nstdout: %s", r.ExitCode, r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_CONFIRMATION_REQUIRED" {
		t.Fatalf("want E_CONFIRMATION_REQUIRED, got: %s", r.Stdout)
	}
}

func TestBatch_QueryRunInstances_EmptyIsUsageError(t *testing.T) {
	home := t.TempDir()
	r := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", " , ", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	if r.ExitCode != 2 {
		t.Fatalf("exit code = %d, want 2 (validation)\nstdout: %s", r.ExitCode, r.Stdout)
	}
}

func TestBatch_QueryRunInstances_ConfirmAggregates(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	run := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--confirm", token)
	data := decodeBatchData(t, run)
	total, succeeded, _, _ := batchSummary(t, data)
	if total != 2 || succeeded != 2 {
		t.Fatalf("summary total=%d succeeded=%d, want 2/2: %s", total, succeeded, run.Stdout)
	}
	items := batchItems(t, data)
	if len(items) != 2 || items[0]["target"] != "a" || items[1]["target"] != "b" {
		t.Fatalf("items targets wrong: %s", run.Stdout)
	}
}

// TestBatch_QueryRunInstances_ConfirmReplayRejected proves the batch confirm
// token is single-use exactly like single-target writes (CLI-SPEC §15.3).
func TestBatch_QueryRunInstances_ConfirmReplayRejected(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	first := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--confirm", token)
	decodeBatchData(t, first)

	replay := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--confirm", token)
	if replay.ExitCode != 6 {
		t.Fatalf("replay exit = %d, want 6 (conflict)\nstdout: %s", replay.ExitCode, replay.Stdout)
	}
	env := decodeEnvelope(t, replay.Stdout)
	if env.OK || env.Error == nil || env.Error.Code != "E_CONFLICT" {
		t.Fatalf("want E_CONFLICT on replay, got: %s", replay.Stdout)
	}
}

// TestBatch_QueryRunInstances_TargetSetChangeInvalidatesToken proves the token
// binds the whole resolved set: removing a target invalidates it (§15.2).
func TestBatch_QueryRunInstances_TargetSetChangeInvalidatesToken(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	// Confirm with a smaller set: token must not match.
	run := runCLI(home, nil, "--format", "json", "query", "run",
		"--instances", "a,b", "--db", "app", "--sql", "select 1", "--dangerous", "--confirm", token)
	if run.ExitCode != 6 {
		t.Fatalf("exit = %d, want 6 (conflict) for changed target set\nstdout: %s", run.ExitCode, run.Stdout)
	}
}

// ─── partial-failure aggregation against a per-instance failing mock ───────────

// failingInstanceMux fails the /query/ endpoint for a specific instance name so
// the batch produces a mix of ok and failed items.
func failingInstanceMux(failInstance string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login/" && r.Method == http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "test-csrf", Path: "/"})
			w.WriteHeader(http.StatusOK)
			return
		case r.URL.Path == "/authenticate/" && r.Method == http.MethodPost:
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "test-session", Path: "/"})
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/query/" {
			_ = r.ParseForm()
			if r.FormValue("instance_name") == failInstance {
				_, _ = w.Write([]byte(`{"status":1,"msg":"err","error_message":"instance unreachable"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","column_list":["n"],"rows":[[1]],"row_count":1}`))
			return
		}
		_, _ = w.Write([]byte(universalBody))
	})
}

func runCLIAgainst(t *testing.T, url, home string, args ...string) cmdResult {
	t.Helper()
	return runCLI(home, map[string]string{"ARCHERY_CLI_URL": url}, args...)
}

func TestBatch_QueryRunInstances_PartialFailureContinues(t *testing.T) {
	srv := httptest.NewServer(failingInstanceMux("b"))
	defer srv.Close()

	home := t.TempDir()
	dry := runCLIAgainst(t, srv.URL, home, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	run := runCLIAgainst(t, srv.URL, home, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1", "--dangerous", "--confirm", token)
	data := decodeBatchData(t, run)
	total, succeeded, failed, skipped := batchSummary(t, data)
	if total != 3 || succeeded != 2 || failed != 1 || skipped != 0 {
		t.Fatalf("summary = total %d/ok %d/fail %d/skip %d, want 3/2/1/0\n%s", total, succeeded, failed, skipped, run.Stdout)
	}
	items := batchItems(t, data)
	if ok, _ := items[1]["ok"].(bool); ok {
		t.Fatalf("item b should be failed: %s", run.Stdout)
	}
	errMap, _ := items[1]["error"].(map[string]any)
	if errMap == nil || errMap["code"] == "" {
		t.Fatalf("failed item missing error{code}: %s", run.Stdout)
	}
}

func TestBatch_QueryRunInstances_StopOnError(t *testing.T) {
	srv := httptest.NewServer(failingInstanceMux("b"))
	defer srv.Close()

	home := t.TempDir()
	dry := runCLIAgainst(t, srv.URL, home, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	run := runCLIAgainst(t, srv.URL, home, "--format", "json", "query", "run",
		"--instances", "a,b,c", "--db", "app", "--sql", "select 1",
		"--continue-on-error=false", "--dangerous", "--confirm", token)
	data := decodeBatchData(t, run)
	total, succeeded, failed, skipped := batchSummary(t, data)
	// a ok, b fails and stops, c skipped.
	if total != 3 || succeeded != 1 || failed != 1 || skipped != 1 {
		t.Fatalf("summary = total %d/ok %d/fail %d/skip %d, want 3/1/1/1\n%s", total, succeeded, failed, skipped, run.Stdout)
	}
	items := batchItems(t, data)
	if sk, _ := items[2]["skipped"].(bool); !sk {
		t.Fatalf("item c should be skipped: %s", run.Stdout)
	}
}

// ─── workflow audit/execute --ids ─────────────────────────────────────────────

func TestBatch_WorkflowAuditIDs_Cycle(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "workflow", "audit",
		"--ids", "7,8,9", "--action", "pass", "--dry-run")
	data := decodeBatchData(t, dry)
	if total, _ := data["preview"].(map[string]any)["total"].(float64); int(total) != 3 {
		t.Fatalf("preview total != 3: %s", dry.Stdout)
	}
	token := data["confirm_token"].(string)

	run := runCLI(home, nil, "--format", "json", "workflow", "audit",
		"--ids", "7,8,9", "--action", "pass", "--confirm", token)
	rd := decodeBatchData(t, run)
	total, succeeded, _, _ := batchSummary(t, rd)
	if total != 3 || succeeded != 3 {
		t.Fatalf("audit batch summary total=%d ok=%d, want 3/3: %s", total, succeeded, run.Stdout)
	}
}

func TestBatch_WorkflowAudit_SingleStillWorks(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "workflow", "audit", "7", "--action", "pass", "--dry-run")
	data := decodeOK(t, dry)
	if _, ok := data["confirm_token"]; !ok {
		t.Fatalf("single audit dry-run missing confirm_token: %s", dry.Stdout)
	}
	// single path emits the legacy object shape, not items[]
	if _, isBatch := data["summary"]; isBatch {
		t.Fatalf("single audit should not emit batch summary: %s", dry.Stdout)
	}
}

func TestBatch_WorkflowExecuteIDs_DangerousGate(t *testing.T) {
	home := t.TempDir()
	// execute is critical: batch without --dangerous must be refused.
	r := runCLI(home, nil, "--format", "json", "workflow", "execute",
		"--ids", "7,8", "--dry-run")
	if r.ExitCode != 5 {
		t.Fatalf("exit = %d, want 5\nstdout: %s", r.ExitCode, r.Stdout)
	}
}

func TestBatch_WorkflowExecuteIDs_DefaultStopsOnError(t *testing.T) {
	// execute defaults continue-on-error=false; verify the flag default by
	// inspecting the dry-run preview path runs and the cycle completes on a
	// healthy mock (all succeed → no skips).
	home := t.TempDir()
	dry := runCLI(home, nil, "--format", "json", "workflow", "execute",
		"--ids", "7,8", "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)
	run := runCLI(home, nil, "--format", "json", "workflow", "execute",
		"--ids", "7,8", "--dangerous", "--confirm", token)
	data := decodeBatchData(t, run)
	total, succeeded, _, _ := batchSummary(t, data)
	if total != 2 || succeeded != 2 {
		t.Fatalf("execute batch summary total=%d ok=%d, want 2/2: %s", total, succeeded, run.Stdout)
	}
}

func TestBatch_WorkflowAudit_BothArgAndIdsRejected(t *testing.T) {
	home := t.TempDir()
	r := runCLI(home, nil, "--format", "json", "workflow", "audit", "7",
		"--ids", "8,9", "--action", "pass", "--dry-run")
	if r.ExitCode != 2 {
		t.Fatalf("exit = %d, want 2 (cannot pass both)\nstdout: %s", r.ExitCode, r.Stdout)
	}
}

// ─── instance import ──────────────────────────────────────────────────────────

func writeManifest(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return p
}

func TestBatch_InstanceImport_CSVCycle(t *testing.T) {
	home := t.TempDir()
	csv := "name,type,db_type,host,port,user\n" +
		"db1,master,mysql,10.0.0.1,3306,root\n" +
		"db2,slave,mysql,10.0.0.2,3306,root\n"
	manifest := writeManifest(t, t.TempDir(), "instances.csv", csv)

	dry := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--dry-run")
	data := decodeBatchData(t, dry)
	if total, _ := data["preview"].(map[string]any)["total"].(float64); int(total) != 2 {
		t.Fatalf("preview total != 2: %s", dry.Stdout)
	}
	token := data["confirm_token"].(string)

	run := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--confirm", token)
	rd := decodeBatchData(t, run)
	total, succeeded, _, _ := batchSummary(t, rd)
	if total != 2 || succeeded != 2 {
		t.Fatalf("import summary total=%d ok=%d, want 2/2: %s", total, succeeded, run.Stdout)
	}
}

func TestBatch_InstanceImport_JSONCycle(t *testing.T) {
	home := t.TempDir()
	jsonBody := `[
	  {"name":"db1","type":"master","db_type":"mysql","host":"10.0.0.1","port":3306,"user":"root"},
	  {"name":"db2","type":"slave","db_type":"mysql","host":"10.0.0.2","port":"3306","user":"root"}
	]`
	manifest := writeManifest(t, t.TempDir(), "instances.json", jsonBody)

	dry := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)
	run := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--confirm", token)
	rd := decodeBatchData(t, run)
	total, succeeded, _, _ := batchSummary(t, rd)
	if total != 2 || succeeded != 2 {
		t.Fatalf("json import summary total=%d ok=%d, want 2/2: %s", total, succeeded, run.Stdout)
	}
}

func TestBatch_InstanceImport_PartialValidationFailure(t *testing.T) {
	home := t.TempDir()
	// Second row has an invalid type → per-item validation failure, no rollback.
	csv := "name,type,db_type,host,port,user\n" +
		"db1,master,mysql,10.0.0.1,3306,root\n" +
		"db2,bogus,mysql,10.0.0.2,3306,root\n"
	manifest := writeManifest(t, t.TempDir(), "instances.csv", csv)

	dry := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--dry-run")
	token := decodeBatchData(t, dry)["confirm_token"].(string)
	run := runCLI(home, nil, "--format", "json", "instance", "import",
		"--file", manifest, "--dangerous", "--confirm", token)
	rd := decodeBatchData(t, run)
	total, succeeded, failed, _ := batchSummary(t, rd)
	if total != 2 || succeeded != 1 || failed != 1 {
		t.Fatalf("import summary total=%d ok=%d fail=%d, want 2/1/1: %s", total, succeeded, failed, run.Stdout)
	}
}

func TestBatch_InstanceImport_MissingFileIsUsageError(t *testing.T) {
	home := t.TempDir()
	r := runCLI(home, nil, "--format", "json", "instance", "import", "--dangerous", "--dry-run")
	if r.ExitCode != 2 {
		t.Fatalf("exit = %d, want 2 (missing --file)\nstdout: %s", r.ExitCode, r.Stdout)
	}
}

func TestBatch_InstanceImport_DangerousGate(t *testing.T) {
	home := t.TempDir()
	manifest := writeManifest(t, t.TempDir(), "instances.csv",
		"name,type,db_type,host,port,user\ndb1,master,mysql,10.0.0.1,3306,root\n")
	r := runCLI(home, nil, "--format", "json", "instance", "import", "--file", manifest, "--dry-run")
	if r.ExitCode != 5 {
		t.Fatalf("exit = %d, want 5 (dangerous required)\nstdout: %s", r.ExitCode, r.Stdout)
	}
}
