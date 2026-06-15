package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestListUsers_SessionShape locks the session transport to user.lists: a GET
// to /user/list/ whose body is {"status":0,"data":[...]} with ids rendered as
// strings (bigint_as_string=True). is_superuser is parsed into the row.
func TestListUsers_SessionShape(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":[` +
			`{"id":"1","username":"alice","display":"Alice","email":"a@x.io","is_active":true,"is_staff":true,"is_superuser":true},` +
			`{"id":"2","username":"bob","display":"Bob","email":"","is_active":false,"is_staff":false,"is_superuser":false}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL) // default session mode
	rows, err := c.Users.ListUsers(testCtx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/user/list/" {
		t.Fatalf("request = %s %s, want GET /user/list/", gotMethod, gotPath)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].ID != 1 || rows[0].Username != "alice" || !rows[0].IsSuperuser {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[1].ID != 2 || rows[1].IsActive {
		t.Errorf("row1 = %+v", rows[1])
	}
}

// TestListUsers_SessionStatusError surfaces a non-zero status as an API error.
func TestListUsers_SessionStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":1,"msg":"403 Forbidden"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Users.ListUsers(testCtx); err == nil {
		t.Fatal("expected error for status=1")
	}
}

// TestListUsers_JWTUsesRESTEndpoint confirms jwt mode hits /api/v1/user/ and
// parses the DRF results array.
func TestListUsers_JWTUsesRESTEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"count":1,"results":[{"id":7,"username":"carol","display":"Carol","email":"c@x.io","is_active":true,"is_staff":false}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetMode(ModeJWT)
	rows, err := c.Users.ListUsers(testCtx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if gotPath != "/api/v1/user/" {
		t.Fatalf("path = %s, want /api/v1/user/", gotPath)
	}
	if len(rows) != 1 || rows[0].ID != 7 || rows[0].Username != "carol" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestListResourceGroups_SessionFormAndShape locks the session transport to
// resource_group.group: a POST to /group/group/ carrying limit/offset/search,
// parsing {"total":N,"rows":[{group_id,group_name}]}.
func TestListResourceGroups_SessionFormAndShape(t *testing.T) {
	var gotPath, gotMethod string
	var gotForm map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"total":3,"rows":[{"group_id":"1","group_name":"ops","ding_webhook":""},{"group_id":"2","group_name":"dev","ding_webhook":""}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	rows, total, err := c.Users.ListResourceGroups(testCtx, 20, 0, "o")
	if err != nil {
		t.Fatalf("ListResourceGroups: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/group/group/" {
		t.Fatalf("request = %s %s, want POST /group/group/", gotMethod, gotPath)
	}
	checks := map[string][]string{
		"limit":  {"20"},
		"offset": {"0"},
		"search": {"o"},
	}
	for k, want := range checks {
		if !reflect.DeepEqual(gotForm[k], want) {
			t.Errorf("form[%q] = %v, want %v", k, gotForm[k], want)
		}
	}
	if total != 3 || len(rows) != 2 || rows[0].ID != 1 || rows[0].Name != "ops" {
		t.Fatalf("unexpected result: total=%d rows=%+v", total, rows)
	}
}

// TestAddResourceGroupRelation_FormFields locks the addrelation form to the
// view: group_id, object_type ("0" for user), and object_info as a JSON array
// of stringified ids.
func TestAddResourceGroupRelation_FormFields(t *testing.T) {
	var gotPath string
	var gotForm map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.Users.AddResourceGroupRelation(testCtx, 5, ResourceGroupObjectUser, []int{3, 4})
	if err != nil {
		t.Fatalf("AddResourceGroupRelation: %v", err)
	}
	if gotPath != "/group/addrelation/" {
		t.Fatalf("path = %s, want /group/addrelation/", gotPath)
	}
	checks := map[string][]string{
		"group_id":    {"5"},
		"object_type": {"0"},
		"object_info": {`["3","4"]`},
	}
	for k, want := range checks {
		if !reflect.DeepEqual(gotForm[k], want) {
			t.Errorf("form[%q] = %v, want %v", k, gotForm[k], want)
		}
	}
}

// TestAddResourceGroupRelation_InstanceType maps --type instance to object_type "1".
func TestAddResourceGroupRelation_InstanceType(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotType = r.PostForm.Get("object_type")
		_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.Users.AddResourceGroupRelation(testCtx, 1, ResourceGroupObjectInstance, []int{9}); err != nil {
		t.Fatalf("AddResourceGroupRelation: %v", err)
	}
	if gotType != "1" {
		t.Errorf("object_type = %q, want 1", gotType)
	}
}

// TestAddResourceGroupRelation_StatusError surfaces a non-zero status as an error.
func TestAddResourceGroupRelation_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":1,"msg":"ResourceGroup matching query does not exist."}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.Users.AddResourceGroupRelation(testCtx, 99, ResourceGroupObjectUser, []int{1})
	if err == nil {
		t.Fatal("expected error for status=1")
	}
}
