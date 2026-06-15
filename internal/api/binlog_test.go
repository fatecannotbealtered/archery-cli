package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestBinlogParse_FormFields locks the my2sql form encoding to the Django view
// in sql/binlog.py: list fields use the suffixed getlist keys (only_schemas,
// only_tables[], sql_type[]), the end time field is stop_time (not end_time),
// and booleans are the literal string "true".
func TestBinlogParse_FormFields(t *testing.T) {
	var got map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":[{"sql":"INSERT INTO t VALUES (1);"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	res, err := c.Binlog.Parse(testCtx, BinlogParseParams{
		InstanceName: "prod",
		StartFile:    "mysql-bin.000123",
		StartPos:     4,
		EndPos:       900,
		StartTime:    "2026-06-15 00:00:00",
		StopTime:     "2026-06-15 01:00:00",
		Schemas:      []string{"db1", "db2"},
		Tables:       []string{"t1", "t2"},
		SQLTypes:     []string{"INSERT", "UPDATE"},
		Num:          10,
		Threads:      2,
		Rollback:     true,
		SaveSQL:      true,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].SQL != "INSERT INTO t VALUES (1);" {
		t.Fatalf("unexpected parse result: %+v", res.Data)
	}

	checks := map[string][]string{
		"instance_name": {"prod"},
		"start_file":    {"mysql-bin.000123"},
		"start_pos":     {"4"},
		"end_pos":       {"900"},
		"start_time":    {"2026-06-15 00:00:00"},
		"stop_time":     {"2026-06-15 01:00:00"},
		"only_schemas":  {"db1", "db2"},
		"only_tables[]": {"t1", "t2"},
		"sql_type[]":    {"INSERT", "UPDATE"},
		"num":           {"10"},
		"threads":       {"2"},
		"rollback":      {"true"},
		"save_sql":      {"true"},
	}
	for key, want := range checks {
		if !reflect.DeepEqual(got[key], want) {
			t.Errorf("form[%q] = %v, want %v", key, got[key], want)
		}
	}
	// The view has no end_time field; ensure we never send one.
	if v, ok := got["end_time"]; ok {
		t.Errorf("form should not carry end_time, got %v", v)
	}
}

func TestBinlogPurge_FormFields(t *testing.T) {
	var got map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"status":0,"msg":"清理成功","data":""}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	res, err := c.Binlog.Purge(testCtx, "7", "mysql-bin.000100")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if res.Status != 0 {
		t.Errorf("status = %d, want 0", res.Status)
	}
	if got["instance_id"][0] != "7" {
		t.Errorf("instance_id = %v, want 7", got["instance_id"])
	}
	if got["binlog"][0] != "mysql-bin.000100" {
		t.Errorf("binlog = %v, want mysql-bin.000100", got["binlog"])
	}
}

func TestBinlogList_ParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":[{"Log_name":"mysql-bin.000001","File_size":1024}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	res, err := c.Binlog.List(testCtx, "prod")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0]["Log_name"] != "mysql-bin.000001" {
		t.Fatalf("unexpected list result: %+v", res.Data)
	}
}
