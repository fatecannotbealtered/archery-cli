package cmd

import (
	"testing"
)

// Payloads below are trimmed captures from Archery v1.8.5
// (/data_dictionary/* on hhyo/archery:v1.8.5).

func TestParseDictTableList_FlattensLetterKeyedMap(t *testing.T) {
	// table_list returns data as a map keyed by the first letter, each value a
	// list of [name, comment] pairs.
	payload := []byte(`{"status":0,"data":{
		"d":[["db","Database privileges"]],
		"c":[["columns_priv","Column privileges"]],
		"e":[["engine_cost",""],["event","Events"]]
	}}`)

	items, err := parseDictTableList(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 tables, got %d", len(items))
	}
	// Sorted by name: columns_priv, db, engine_cost, event.
	wantNames := []string{"columns_priv", "db", "engine_cost", "event"}
	for i, want := range wantNames {
		if got := items[i]["name"]; got != want {
			t.Errorf("items[%d].name = %v, want %s", i, got, want)
		}
	}
	if items[0]["comment"] != "Column privileges" {
		t.Errorf("comment = %v, want Column privileges", items[0]["comment"])
	}
	if items[2]["comment"] != "" {
		t.Errorf("empty comment = %v, want empty string", items[2]["comment"])
	}
}

func TestParseDictTableList_EmptyData(t *testing.T) {
	items, err := parseDictTableList([]byte(`{"status":0,"data":{}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("want 0 tables, got %d", len(items))
	}
}

func TestParseDictTableList_ServerError(t *testing.T) {
	// Non-zero status surfaces as a validation failure (server msg is emitted to
	// the JSON envelope; the returned error is the silent sentinel).
	_, err := parseDictTableList([]byte(`{"status":1,"msg":"Instance.DoesNotExist"}`))
	if err == nil {
		t.Fatal("expected error for status=1")
	}
}

func TestParseDictTableInfo_KeepsColumnRowTables(t *testing.T) {
	// table_info.data carries {column_list, rows} tables plus create_sql.
	payload := []byte(`{"status":0,"data":{
		"meta_data":{"column_list":["table_name","engine"],"rows":["db","MyISAM"]},
		"desc":{"column_list":["列名","列类型"],"rows":[["Host","char(60)"],["Db","char(64)"]]},
		"index":{"column_list":["列名","索引名"],"rows":[["Host","PRIMARY"]]},
		"create_sql":[["db","CREATE TABLE db ..."]]
	}}`)

	info, err := parseDictTableInfo(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"meta_data", "desc", "index", "create_sql"} {
		if _, ok := info[key]; !ok {
			t.Errorf("info missing key %q", key)
		}
	}
	meta, ok := info["meta_data"].(map[string]any)
	if !ok {
		t.Fatalf("meta_data not an object: %T", info["meta_data"])
	}
	if _, ok := meta["column_list"]; !ok {
		t.Error("meta_data missing column_list")
	}
}

func TestParseDictTableInfo_WrongParamRejected(t *testing.T) {
	// Sending table_name instead of tb_name yields the view's 非法调用！ message.
	_, err := parseDictTableInfo([]byte(`{"status":1,"msg":"非法调用！"}`))
	if err == nil {
		t.Fatal("expected error for status=1")
	}
}

func TestDictTableInfoUsesTbNameFlag(t *testing.T) {
	// Guard the param-name fix: the command must define the --table flag that
	// maps to tb_name in the request.
	if dictTableInfoCmd.Flags().Lookup("table") == nil {
		t.Fatal("table-info missing --table flag")
	}
}

func TestToStringSlice_NilAndScalars(t *testing.T) {
	got := toStringSlice([]any{"a", nil, 3})
	want := []string{"a", "", "3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if toStringSlice("not-an-array") != nil {
		t.Error("non-array should yield nil")
	}
}
