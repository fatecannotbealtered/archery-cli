package cmd

import (
	"encoding/json"
	"testing"
)

// TestSlowqueryReviewResponseDecode locks the v1.8.5 slowquery_review wire shape:
// a bare {total, rows:[...]} with PascalCase aggregate columns, remapped onto the
// CLI's snake_case schema. The Sum() aggregates over FloatField columns arrive as
// floats on the wire (42.0/1000.0/420.0), while the *Avg pair is int()-cast.
func TestSlowqueryReviewResponseDecode(t *testing.T) {
	raw := `{"total":2,"rows":[
		{"SQLId":"abc123","SQLText":"select * from users where id = ?","DBName":"app",
		 "CreateTime":"2026-06-07 10:00:00","QueryTimeAvg":1.234567,
		 "MySQLTotalExecutionCounts":42.0,"MySQLTotalExecutionTimes":51.85,
		 "ParseTotalRowCounts":1000.0,"ReturnTotalRowCounts":420.0,
		 "ParseRowAvg":24,"ReturnRowAvg":10}
	]}`
	var resp slowqueryReviewResponse
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
	if r.SQLId != "abc123" || r.DBName != "app" {
		t.Errorf("SQLId/DBName = %q/%q", r.SQLId, r.DBName)
	}
	if r.MySQLTotalExecutionCounts != 42 || r.ParseRowAvg != 24 || r.ReturnRowAvg != 10 {
		t.Errorf("counts = %v/%d/%d", r.MySQLTotalExecutionCounts, r.ParseRowAvg, r.ReturnRowAvg)
	}

	m := slowqueryReviewRowToMap(r)
	// last_seen is a time field: normalizeAgentMap rewrites it to UTC RFC3339.
	if m["sql_id"] != "abc123" || m["db_name"] != "app" || m["last_seen"] != "2026-06-07T10:00:00Z" {
		t.Errorf("remap basics = %v", m)
	}
	if m["mysql_total"] != float64(42) || m["rows_examined_avg"] != 24 || m["rows_sent_avg"] != 10 {
		t.Errorf("remap counts = %v", m)
	}
	if m["query_time_avg"] != 1.234567 {
		t.Errorf("query_time_avg = %v", m["query_time_avg"])
	}
}

// TestSlowqueryReviewPermissionError locks the failure branch: when the user's
// group is not associated with the instance the view returns {status:1,msg}.
func TestSlowqueryReviewPermissionError(t *testing.T) {
	raw := `{"status":1,"msg":"你所在组未关联该实例","data":[]}`
	var resp slowqueryReviewResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != 1 || resp.Msg == "" {
		t.Fatalf("expected status=1 with msg, got %+v", resp)
	}
}

// TestSlowqueryHistoryResponseDecode locks the v1.8.5 slowquery_review_history
// wire shape and its per-occurrence PascalCase columns.
func TestSlowqueryHistoryResponseDecode(t *testing.T) {
	raw := `{"total":3,"rows":[
		{"ExecutionStartTime":"2026-06-07 09:55:00","DBName":"app",
		 "HostAddress":"'app'@'10.0.0.1'","SQLText":"select 1",
		 "TotalExecutionCounts":5.0,"QueryTimePct95":0.9,"QueryTimes":4.5,
		 "LockTimes":0.01,"ParseRowCounts":50.0,"ReturnRowCounts":5.0}
	]}`
	var resp slowqueryHistoryResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 3 || len(resp.Rows) != 1 {
		t.Fatalf("total/rows = %d/%d", resp.Total, len(resp.Rows))
	}
	r := resp.Rows[0]
	m := slowqueryHistoryRowToMap(r)
	// execution_start_time is a time field: normalized to UTC RFC3339.
	if m["execution_start_time"] != "2026-06-07T09:55:00Z" || m["db_name"] != "app" {
		t.Errorf("remap basics = %v", m)
	}
	if m["host_address"] != "'app'@'10.0.0.1'" {
		t.Errorf("host_address = %v", m["host_address"])
	}
	if m["total_execution_counts"] != float64(5) || m["rows_examined"] != float64(50) || m["rows_sent"] != float64(5) {
		t.Errorf("remap counts = %v", m)
	}
	if m["query_time_pct95"] != 0.9 || m["lock_times"] != 0.01 {
		t.Errorf("remap times = %v", m)
	}
}

// TestDecodeOptimizeData covers the polymorphic `data` field: sqladvisor/soar
// emit a string report, sqltuning emits a nested object, and null degrades to "".
func TestDecodeOptimizeData(t *testing.T) {
	if got := decodeOptimizeData(json.RawMessage(`"-- add an index on users(id)"`)); got != "-- add an index on users(id)" {
		t.Errorf("string payload = %v", got)
	}
	if got := decodeOptimizeData(json.RawMessage(`null`)); got != "" {
		t.Errorf("null payload = %v, want empty string", got)
	}
	if got := decodeOptimizeData(nil); got != "" {
		t.Errorf("empty payload = %v, want empty string", got)
	}
	obj := decodeOptimizeData(json.RawMessage(`{"sqltext":"select 1","plan":[{"id":1}]}`))
	m, ok := obj.(map[string]any)
	if !ok {
		t.Fatalf("tuning payload = %T, want map", obj)
	}
	if m["sqltext"] != "select 1" {
		t.Errorf("tuning sqltext = %v", m["sqltext"])
	}
}

// TestFormatSlowCount locks the count rendering: counts arrive as floats
// (Sum over FloatField, e.g. 42.0) and must render as plain integers when whole,
// degrading to a trimmed float only for the rare fractional aggregate.
func TestFormatSlowCount(t *testing.T) {
	cases := map[float64]string{
		42:    "42",
		0:     "0",
		8400:  "8400",
		1.5:   "1.5",
		100.0: "100",
	}
	for in, want := range cases {
		if got := formatSlowCount(in); got != want {
			t.Errorf("formatSlowCount(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestSlowqueryDate locks the date truncation: the v1.8.5 views parse EndTime
// with strptime(..., '%Y-%m-%d') and 500 on a trailing time component, so a
// datetime argument must be reduced to its date prefix before it is sent.
func TestSlowqueryDate(t *testing.T) {
	cases := map[string]string{
		"2026-06-15 23:59:59": "2026-06-15",
		"2026-06-15T23:59:59": "2026-06-15",
		"  2026-06-15  ":      "2026-06-15",
		"2026-06-15":          "2026-06-15",
		"":                    "",
	}
	for in, want := range cases {
		if got := slowqueryDate(in); got != want {
			t.Errorf("slowqueryDate(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSlowqueryOptimizeFlags guards the per-tool flags exist with the right
// defaults so the payload builder can pick the correct upstream field names.
func TestSlowqueryOptimizeFlags(t *testing.T) {
	cmd := slowqueryOptimizeCmd
	if tool, _ := cmd.Flags().GetString("tool"); tool != "soar" {
		t.Errorf("default tool = %q, want soar", tool)
	}
	if f := cmd.Flags().Lookup("verbose"); f == nil || f.Value.Type() != "bool" {
		t.Errorf("verbose flag missing or wrong type")
	}
	opt := cmd.Flags().Lookup("option")
	if opt == nil || opt.Value.Type() != "stringSlice" {
		t.Fatalf("option flag missing or wrong type")
	}
	got, _ := cmd.Flags().GetStringSlice("option")
	want := []string{"sys_parm", "sql_plan", "obj_stat"}
	if len(got) != len(want) {
		t.Fatalf("default option = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
