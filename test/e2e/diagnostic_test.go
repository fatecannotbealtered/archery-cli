package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordedRequest captures the path and parsed form of a diagnostic request so
// tests can assert the CLI hits the exact v1.8.5 route with the exact field
// names the Django views read (ThreadIDs as a JSON array, the misspelled
// "tablesapce" path, etc.).
type recordedRequest struct {
	Path string
	Form map[string]string
}

// diagnosticMux replays the db_diagnostic views' real response shapes
// ({status,msg,rows}) and records every request. statusErrPath, when non-empty,
// is answered with {"status":1,...} to exercise the error path.
func diagnosticMux(rec *[]recordedRequest, mu *sync.Mutex, statusErrPath string) http.Handler {
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

		_ = r.ParseForm()
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		mu.Lock()
		*rec = append(*rec, recordedRequest{Path: r.URL.Path, Form: form})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if statusErrPath != "" && r.URL.Path == statusErrPath {
			_, _ = w.Write([]byte(`{"status":1,"msg":"你所在组未关联该实例","data":[]}`))
			return
		}
		switch r.URL.Path {
		case "/db_diagnostic/create_kill_session/":
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":"kill 101;kill 102;"}`))
		case "/db_diagnostic/kill_session/":
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":[]}`))
		default:
			// process / tablesapce / trxandlocks / innodb_trx all return rows.
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","rows":[{"id":101,"user":"app","Info":"select 1"}]}`))
		}
	})
}

func findRequest(rec []recordedRequest, path string) (recordedRequest, bool) {
	for _, r := range rec {
		if r.Path == path {
			return r, true
		}
	}
	return recordedRequest{}, false
}

func TestDiagnostic_Process_DefaultsCommandTypeAll(t *testing.T) {
	var rec []recordedRequest
	var mu sync.Mutex
	srv := httptest.NewServer(diagnosticMux(&rec, &mu, ""))
	defer srv.Close()

	home := t.TempDir()
	res := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", "process", "--instance", "inst1")
	decodeOK(t, res)

	req, ok := findRequest(rec, "/db_diagnostic/process/")
	if !ok {
		t.Fatalf("process endpoint not hit; recorded=%+v", rec)
	}
	if req.Form["instance_name"] != "inst1" {
		t.Errorf("instance_name = %q, want inst1", req.Form["instance_name"])
	}
	if req.Form["command_type"] != "All" {
		t.Errorf("command_type = %q, want All (default)", req.Form["command_type"])
	}
}

func TestDiagnostic_Tablespace_UsesMisspelledRoute(t *testing.T) {
	var rec []recordedRequest
	var mu sync.Mutex
	srv := httptest.NewServer(diagnosticMux(&rec, &mu, ""))
	defer srv.Close()

	home := t.TempDir()
	res := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", "tablespace", "--instance", "inst1")
	decodeOK(t, res)

	if _, ok := findRequest(rec, "/db_diagnostic/tablesapce/"); !ok {
		t.Fatalf("tablespace must hit misspelled /db_diagnostic/tablesapce/; recorded=%+v", rec)
	}
	if _, ok := findRequest(rec, "/db_diagnostic/tablespace/"); ok {
		t.Fatalf("must NOT hit the corrected spelling; v1.8.5 route is misspelled")
	}
}

func TestDiagnostic_Locks_And_Transactions_RowEndpoints(t *testing.T) {
	for _, tc := range []struct{ sub, path string }{
		{"locks", "/db_diagnostic/trxandlocks/"},
		{"transactions", "/db_diagnostic/innodb_trx/"},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			var rec []recordedRequest
			var mu sync.Mutex
			srv := httptest.NewServer(diagnosticMux(&rec, &mu, ""))
			defer srv.Close()

			home := t.TempDir()
			res := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", tc.sub, "--instance", "inst1")
			decodeOK(t, res)
			if _, ok := findRequest(rec, tc.path); !ok {
				t.Fatalf("%s must hit %s; recorded=%+v", tc.sub, tc.path, rec)
			}
		})
	}
}

func TestDiagnostic_StatusErrorSurfaced(t *testing.T) {
	var rec []recordedRequest
	var mu sync.Mutex
	srv := httptest.NewServer(diagnosticMux(&rec, &mu, "/db_diagnostic/process/"))
	defer srv.Close()

	home := t.TempDir()
	res := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", "process", "--instance", "inst1")
	if res.ExitCode == 0 {
		t.Fatalf("status:1 must produce a non-zero exit; stdout=%s", res.Stdout)
	}
	env := decodeEnvelope(t, res.Stdout)
	if env.OK {
		t.Fatalf("expected ok=false on status:1; stdout=%s", res.Stdout)
	}
	if env.Error == nil || env.Error.Code != "E_VALIDATION" {
		t.Fatalf("expected E_VALIDATION error code; got %+v", env.Error)
	}
}

func TestDiagnostic_Kill_TwoStepThreadIDsJSONArray(t *testing.T) {
	var rec []recordedRequest
	var mu sync.Mutex
	srv := httptest.NewServer(diagnosticMux(&rec, &mu, ""))
	defer srv.Close()

	home := t.TempDir()
	// dry-run: previews via create_kill_session, no kill_session execution yet.
	dry := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", "kill",
		"--instance", "inst1", "--threads", "101,102", "--dangerous", "--dry-run")
	if dry.ExitCode != 0 {
		t.Fatalf("dry-run exit = %d, want 0; stderr=%s", dry.ExitCode, dry.Stderr)
	}
	creq, ok := findRequest(rec, "/db_diagnostic/create_kill_session/")
	if !ok {
		t.Fatalf("dry-run must preview via create_kill_session; recorded=%+v", rec)
	}
	// ThreadIDs must be a JSON array string, not a comma-joined string.
	var ids []int
	if err := json.Unmarshal([]byte(creq.Form["ThreadIDs"]), &ids); err != nil {
		t.Fatalf("ThreadIDs %q is not a JSON array: %v", creq.Form["ThreadIDs"], err)
	}
	if len(ids) != 2 || ids[0] != 101 || ids[1] != 102 {
		t.Fatalf("ThreadIDs decoded = %v, want [101 102]", ids)
	}
	if _, killed := findRequest(rec, "/db_diagnostic/kill_session/"); killed {
		t.Fatalf("dry-run must NOT execute kill_session")
	}
	token := decodeBatchData(t, dry)["confirm_token"].(string)

	// confirm: executes kill_session with the same JSON-array ThreadIDs.
	// Hold the mux mutex: a keep-alive handler goroutine from the dry-run may
	// still touch rec, so resetting it unguarded races with the append at the
	// top of diagnosticMux.
	mu.Lock()
	rec = nil
	mu.Unlock()
	run := runCLIAgainst(t, srv.URL, home, "--format", "json", "diagnostic", "kill",
		"--instance", "inst1", "--threads", "101,102", "--dangerous", "--confirm", token)
	if run.ExitCode != 0 {
		t.Fatalf("confirm exit = %d, want 0; stderr=%s\nstdout=%s", run.ExitCode, run.Stderr, run.Stdout)
	}
	kreq, ok := findRequest(rec, "/db_diagnostic/kill_session/")
	if !ok {
		t.Fatalf("confirm must execute kill_session; recorded=%+v", rec)
	}
	if err := json.Unmarshal([]byte(kreq.Form["ThreadIDs"]), &ids); err != nil {
		t.Fatalf("kill_session ThreadIDs %q is not a JSON array: %v", kreq.Form["ThreadIDs"], err)
	}
}

func TestDiagnostic_Kill_RejectsNonIntegerThreads(t *testing.T) {
	home := t.TempDir()
	res := runCLIAgainst(t, mockURL, home, "--format", "json", "diagnostic", "kill",
		"--instance", "inst1", "--threads", "101,abc", "--dangerous", "--dry-run")
	if res.ExitCode == 0 {
		t.Fatalf("non-integer thread id must be rejected; stdout=%s", res.Stdout)
	}
}
