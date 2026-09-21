package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestSubmit_UsesRESTShapeOnSessionTransport locks the submit contract that the
// v1.9.0 route removal forced: the request goes to the DRF endpoint even on the
// session transport, travels over that transport rather than the bearer one,
// and carries the nested WorkflowContentSerializer shape.
//
// Three details here are easy to get wrong and invisible until a live server
// rejects them: the instance FK is "instance" (not "instance_id"), sql_content
// sits beside "workflow" rather than inside it, and the workflow id comes back
// as workflow_id — the top-level "id" is the content row.
func TestSubmit_UsesRESTShapeOnSessionTransport(t *testing.T) {
	var gotPath, gotContentType, gotRequestedWith string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/":
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf", Path: "/"})
			return
		case "/authenticate/":
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sid", Path: "/"})
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
			return
		}
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotRequestedWith = r.Header.Get("X-Requested-With")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"workflow_id":42,"workflow":{"id":42},"sql_content":"x"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeSession)

	resp, err := c.Workflows.Submit(testCtx, WorkflowSubmitRequest{
		Name:       "add column c",
		InstanceID: 3,
		DBName:     "db1",
		SQLContent: "alter table t add c int",
		GroupID:    5,
		IsBackup:   true,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if gotPath != "/api/v1/workflow/" {
		t.Fatalf("path = %q, want /api/v1/workflow/", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	// X-Requested-With is set only by the session transport's header builder,
	// so it proves the request left over the session path rather than the
	// bearer one. (Cookie propagation itself is covered in client_test.go; this
	// client is marked session-ready without a real login.)
	if gotRequestedWith != "XMLHttpRequest" {
		t.Fatalf("X-Requested-With = %q, want the session transport to have sent it", gotRequestedWith)
	}

	wf, ok := gotBody["workflow"].(map[string]any)
	if !ok {
		t.Fatalf("body has no nested workflow object: %v", gotBody)
	}
	if gotBody["sql_content"] != "alter table t add c int" {
		t.Fatalf("sql_content = %v, want it at the top level", gotBody["sql_content"])
	}
	if wf["instance"] != float64(3) {
		t.Fatalf("workflow.instance = %v, want 3 (the FK key is 'instance')", wf["instance"])
	}
	if _, present := wf["instance_id"]; present {
		t.Fatalf("workflow carries instance_id, which the serializer ignores: %v", wf)
	}
	if wf["group_id"] != float64(5) {
		t.Fatalf("workflow.group_id = %v, want 5", wf["group_id"])
	}
	if wf["is_backup"] != true {
		t.Fatalf("workflow.is_backup = %v, want true", wf["is_backup"])
	}

	if resp.ID != 42 {
		t.Fatalf("ID = %d, want 42 (workflow_id, not the content row's id 7)", resp.ID)
	}
}

// TestSubmit_FallsBackToNestedWorkflowID covers a response that omits the
// top-level workflow_id: the id is then read from the nested workflow object.
func TestSubmit_FallsBackToNestedWorkflowID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"workflow":{"id":99}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	resp, err := c.Workflows.Submit(testCtx, WorkflowSubmitRequest{Name: "n", InstanceID: 1, GroupID: 1})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if resp.ID != 99 {
		t.Fatalf("ID = %d, want 99 from the nested workflow object", resp.ID)
	}
}

// TestSQLCheck_SendsFullSQLAndParsesRows locks the pre-check contract: the SQL
// field is named full_sql (the removed /simplecheck/ view took sql_content), and
// the response rows are ReviewResult objects whose errlevel drives the level
// label the CLI renders.
func TestSQLCheck_SendsFullSQLAndParsesRows(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		// `status` as a bare number: Archery declares it a CharField but the
		// engines emit an int. The decoder must not bind fields it never reads.
		_, _ = w.Write([]byte(`{"status":0,"warning_count":1,"error_count":0,"rows":[` +
			`{"id":1,"stage":"CHECKED","errlevel":1,"stagestatus":"Audit completed",` +
			`"errormessage":"Keyword used","sql":"alter table t add c int","affected_rows":0}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	results, err := c.Workflows.SQLCheck(testCtx, WorkflowSQLCheckRequest{
		InstanceID: 3,
		DBName:     "db1",
		SQLContent: "alter table t add c int",
	})
	if err != nil {
		t.Fatalf("SQLCheck: %v", err)
	}

	if gotPath != "/api/v1/workflow/sqlcheck/" {
		t.Fatalf("path = %q, want /api/v1/workflow/sqlcheck/", gotPath)
	}
	if gotBody["full_sql"] != "alter table t add c int" {
		t.Fatalf("full_sql = %v, want the SQL (not sql_content)", gotBody["full_sql"])
	}
	if _, present := gotBody["sql_content"]; present {
		t.Fatalf("body carries sql_content, which this endpoint ignores: %v", gotBody)
	}
	if gotBody["instance_id"] != float64(3) {
		t.Fatalf("instance_id = %v, want 3", gotBody["instance_id"])
	}

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ErrLevel != 1 || results[0].Level != "warning" {
		t.Fatalf("errlevel=%d level=%q, want 1/warning", results[0].ErrLevel, results[0].Level)
	}
	if results[0].Message != "Keyword used" {
		t.Fatalf("Message = %q", results[0].Message)
	}
}

// TestNotFoundNamesTheRoute guards the diagnostic that cost an afternoon when
// Archery dropped /autoreview/: a 404 with no body used to surface as a bare
// "resource not found", which reads like a bad parameter. It must name the
// route that was missing.
func TestNotFoundNamesTheRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	_, err := c.Workflows.SQLCheck(testCtx, WorkflowSQLCheckRequest{InstanceID: 1, DBName: "db1"})
	if err == nil {
		t.Fatal("SQLCheck: want an error on 404")
	}
	msg := err.Error()
	if !strings.Contains(msg, "/api/v1/workflow/sqlcheck/") {
		t.Fatalf("404 message does not name the route: %q", msg)
	}
	if !strings.Contains(msg, "POST") {
		t.Fatalf("404 message does not name the method: %q", msg)
	}
}

// TestSQLCheck_UndecodableResponseIsNonRetryable pins two properties of a
// response the decoder cannot read. It must not be reported as a network
// problem — a contract mismatch does not improve on retry — and above all it
// must not be swallowed: the transport this replaced tolerated several
// envelopes by ignoring decode errors and returning an empty slice, so a
// pre-check that failed to parse rendered as "no findings", which for a
// pre-production SQL gate is the most dangerous answer available.
func TestSQLCheck_UndecodableResponseIsNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rows": "this should have been a list"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	results, err := c.Workflows.SQLCheck(testCtx, WorkflowSQLCheckRequest{InstanceID: 1, DBName: "db1"})
	if err == nil {
		t.Fatalf("want an error, got %d results — a decode failure must never read as a clean check", len(results))
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError so the cmd layer maps it to E_UNKNOWN", err)
	}
	if apiErr.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0: a status would make this retryable", apiErr.StatusCode)
	}
}

// TestSubmit_UndecodableResponseIsNonRetryable is the submit-side counterpart.
func TestSubmit_UndecodableResponseIsNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_id": "not a number"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	if _, err := c.Workflows.Submit(testCtx, WorkflowSubmitRequest{Name: "n", InstanceID: 1, GroupID: 1}); err == nil {
		t.Fatal("want an error on an undecodable submit response")
	} else if apiErr, ok := err.(*APIError); !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	} else if apiErr.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0", apiErr.StatusCode)
	}
}

// TestSubmit_MissingWorkflowIDIsAnError guards the quiet failure: a 2xx whose
// body carries no workflow id at all must not report success with id 0, which
// downstream would read as a real workflow to audit or execute.
func TestSubmit_MissingWorkflowIDIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"sql_content":"select 1"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)

	resp, err := c.Workflows.Submit(testCtx, WorkflowSubmitRequest{Name: "n", InstanceID: 1, GroupID: 1})
	if err == nil {
		t.Fatalf("want an error, got resp with ID=%d", resp.ID)
	}
	if !strings.Contains(err.Error(), "no workflow id") {
		t.Fatalf("error should say the id is missing, got: %v", err)
	}
}
