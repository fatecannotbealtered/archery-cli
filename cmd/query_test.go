package cmd

import (
	"encoding/json"
	"testing"
)

func TestQueryRunFlags(t *testing.T) {
	cmd := queryRunCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "instance",
			flagName: "instance",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("instance")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db",
			flagName: "db",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "sql",
			flagName: "sql",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("sql")
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
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
				}
			},
		},
		{
			name:     "table",
			flagName: "table",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("table")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "schema",
			flagName: "schema",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("schema")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
				return
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

func TestQueryExplainFlags(t *testing.T) {
	cmd := queryExplainCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "instance",
			flagName: "instance",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("instance")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db",
			flagName: "db",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "sql",
			flagName: "sql",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("sql")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
				return
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

// TestQueryLogResponseDecode locks the v1.8.5 wire shape: _querylog emits
// {total, rows:[...]} at the top level (no {status,msg,data} envelope), and each
// row uses user_display / db_name / create_time, which the CLI remaps onto
// username / db / exec_time.
func TestQueryLogResponseDecode(t *testing.T) {
	raw := `{"total":2,"rows":[
		{"id":7,"instance_name":"prod","db_name":"app","sqllog":"SELECT 1","effect_row":1,"cost_time":"0.01","user_display":"Alice","favorite":true,"alias":"smoke","create_time":"2026-06-15 10:00:00"}
	]}`
	var resp queryLogResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("Rows len = %d, want 1", len(resp.Rows))
	}
	r := resp.Rows[0]
	if r.UserDisp != "Alice" {
		t.Errorf("UserDisp = %q, want Alice", r.UserDisp)
	}
	if r.DbName != "app" {
		t.Errorf("DbName = %q, want app", r.DbName)
	}
	if r.CreateTime != "2026-06-15 10:00:00" {
		t.Errorf("CreateTime = %q", r.CreateTime)
	}
	if !r.Favorite || r.Alias != "smoke" {
		t.Errorf("favorite/alias = %v/%q", r.Favorite, r.Alias)
	}
}

// TestQueryExplainResponseDecode locks the to_sep_dict() shape: on success data
// is a single {column_list, rows} object, decoded in two steps (status first,
// then the typed payload) so that a rejection — where the view returns data=[]
// (array) with status=1 — surfaces the server message instead of an unmarshal
// error against the object shape.
func TestQueryExplainResponseDecode(t *testing.T) {
	raw := `{"status":0,"msg":"ok","data":{"column_list":["id","type"],"rows":[[1,"ALL"],[2,"ref"]]}}`
	var resp queryExplainResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var dataSet queryExplainDataSet
	if err := json.Unmarshal(resp.Data, &dataSet); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(dataSet.ColumnList) != 2 || len(dataSet.Rows) != 2 {
		t.Fatalf("decoded columns/rows = %d/%d, want 2/2", len(dataSet.ColumnList), len(dataSet.Rows))
	}

	// Rejection path: data is an empty array. The outer envelope must still decode
	// cleanly so the status check can run before any attempt to bind the payload.
	rejected := `{"status":1,"msg":"仅支持explain开头的语句，请检查","data":[]}`
	var rresp queryExplainResponse
	if err := json.Unmarshal([]byte(rejected), &rresp); err != nil {
		t.Fatalf("unmarshal rejected envelope: %v", err)
	}
	if rresp.Status != 1 || rresp.Msg == "" {
		t.Fatalf("rejected status/msg = %d/%q, want 1/non-empty", rresp.Status, rresp.Msg)
	}
}

// TestZipExplainRows verifies the column-separated dict is reassembled into one
// map per row, with over-long rows truncated and short rows leaving keys unset.
func TestZipExplainRows(t *testing.T) {
	cols := []string{"id", "type"}
	plan := zipExplainRows(cols, [][]any{{1.0, "ALL"}, {2.0}, {3.0, "ref", "extra"}})
	if len(plan) != 3 {
		t.Fatalf("plan len = %d, want 3", len(plan))
	}
	if plan[0]["id"] != 1.0 || plan[0]["type"] != "ALL" {
		t.Errorf("row0 = %v", plan[0])
	}
	if _, ok := plan[1]["type"]; ok {
		t.Errorf("row1 should leave 'type' unset, got %v", plan[1])
	}
	if _, ok := plan[2]["extra"]; ok {
		t.Errorf("row2 should drop overflow cell, got %v", plan[2])
	}
}

// TestZipExplainRowsEmpty covers the degenerate cases: no rows and no columns.
func TestZipExplainRowsEmpty(t *testing.T) {
	if got := zipExplainRows([]string{"a"}, nil); len(got) != 0 {
		t.Errorf("no rows -> len %d, want 0", len(got))
	}
	plan := zipExplainRows(nil, [][]any{{1, 2}})
	if len(plan) != 1 || len(plan[0]) != 0 {
		t.Errorf("no columns -> %v, want one empty map", plan)
	}
}
