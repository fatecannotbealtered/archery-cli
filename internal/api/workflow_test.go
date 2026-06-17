package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDetail_SessionCombinesResultAndMetadata verifies the reworked Detail():
// it reads execution/review rows from /sqlworkflow/detail_content/ into Result,
// reconstructs SQLContent from those rows, and backfills the metadata (title,
// status code, instance/group names) from the session list endpoint. This is
// the data that lets `workflow detail` show why an execution failed.
func TestDetail_SessionCombinesResultAndMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sqlworkflow/detail_content/":
			_, _ = w.Write([]byte(`{"rows":[` +
				`{"id":1,"stage":"Executed","errlevel":2,"stagestatus":"异常终止",` +
				`"errormessage":"Invalid remote backup information","sql":"alter table t add c int",` +
				`"affected_rows":0,"execute_time":"0.000"}]}`))
		case "/sqlworkflow_list/":
			_, _ = w.Write([]byte(`{"total":1,"rows":[` +
				`{"id":"42","workflow_name":"add column c","status":"workflow_exception",` +
				`"engineer_display":"alice","instance__instance_name":"inst1","db_name":"db1",` +
				`"group_name":"ops","create_time":"2026-06-15 10:00:00"}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeSession)

	detail, err := c.Workflows.Detail(testCtx, 42)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	if len(detail.Result) != 1 {
		t.Fatalf("Result len = %d, want 1", len(detail.Result))
	}
	if got := detail.Result[0].ErrorMessage; got != "Invalid remote backup information" {
		t.Fatalf("Result[0].ErrorMessage = %q", got)
	}
	if detail.Result[0].ErrLevel != 2 {
		t.Fatalf("Result[0].ErrLevel = %d, want 2", detail.Result[0].ErrLevel)
	}
	if detail.SQLContent != "alter table t add c int" {
		t.Fatalf("SQLContent = %q, want the reconstructed SQL", detail.SQLContent)
	}
	// Metadata backfilled from the list row matched by id.
	if detail.Title != "add column c" {
		t.Fatalf("Title = %q, want 'add column c'", detail.Title)
	}
	if detail.StatusCode != "workflow_exception" {
		t.Fatalf("StatusCode = %q, want workflow_exception", detail.StatusCode)
	}
	if detail.InstanceName != "inst1" || detail.GroupName != "ops" {
		t.Fatalf("InstanceName=%q GroupName=%q, want inst1/ops", detail.InstanceName, detail.GroupName)
	}
}

// TestDetail_SessionNumericExecuteTime guards the real-world shape that broke
// `workflow detail 42594` live: Archery returns execute_time (and sometimes
// sequence) as a bare JSON number for an executed workflow, not a string. The
// row must still parse and render the value as a string.
func TestDetail_SessionNumericExecuteTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sqlworkflow/detail_content/":
			// execute_time as a NUMBER (0.008516) and sequence as a NUMBER.
			_, _ = w.Write([]byte(`{"rows":[` +
				`{"id":1,"stage":"Executed","errlevel":0,"stagestatus":"Execute Successfully",` +
				`"errormessage":"None","sql":"DEL pangu:sso:token","affected_rows":0,` +
				`"sequence":12,"execute_time":0.008516}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"total":0,"rows":[]}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeSession)

	detail, err := c.Workflows.Detail(testCtx, 42594)
	if err != nil {
		t.Fatalf("Detail with numeric execute_time must parse, got: %v", err)
	}
	if len(detail.Result) != 1 {
		t.Fatalf("Result len = %d, want 1", len(detail.Result))
	}
	if got := string(detail.Result[0].ExecuteTime); got != "0.008516" {
		t.Fatalf("ExecuteTime = %q, want 0.008516", got)
	}
	if got := string(detail.Result[0].Sequence); got != "12" {
		t.Fatalf("Sequence = %q, want 12", got)
	}
	if detail.SQLContent != "DEL pangu:sso:token" {
		t.Fatalf("SQLContent = %q", detail.SQLContent)
	}
}

// TestDetail_SessionFiltersInceptionWrapperSQL confirms the inception_magic
// wrapper statements are excluded from the reconstructed SQLContent.
func TestDetail_SessionFiltersInceptionWrapperSQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sqlworkflow/detail_content/":
			_, _ = w.Write([]byte(`{"rows":[` +
				`{"id":1,"sql":"inception_magic_start"},` +
				`{"id":2,"sql":"update t set x=1"},` +
				`{"id":3,"sql":"inception_magic_commit"}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"total":0,"rows":[]}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeSession)

	detail, err := c.Workflows.Detail(testCtx, 1)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.SQLContent != "update t set x=1" {
		t.Fatalf("SQLContent = %q, want only the user SQL", detail.SQLContent)
	}
	// All rows (including wrappers) stay in Result for full fidelity.
	if len(detail.Result) != 3 {
		t.Fatalf("Result len = %d, want 3 (all rows retained)", len(detail.Result))
	}
}
