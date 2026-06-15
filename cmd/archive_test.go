package cmd

import "testing"

func TestArchiveListFlags(t *testing.T) {
	cmd := archiveListCmd
	for _, name := range []string{"instance", "state", "limit", "offset", "search", "fields"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on archive list", name)
		}
	}
}

func TestArchiveApplyFlags(t *testing.T) {
	cmd := archiveApplyCmd
	for _, name := range []string{
		"title", "group", "src-instance", "src-db", "src-table", "mode",
		"dest-instance", "dest-db", "dest-table", "condition", "no-delete", "sleep",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on archive apply", name)
		}
	}
}

func TestArchiveAuditFlags(t *testing.T) {
	cmd := archiveAuditCmd
	for _, name := range []string{"action", "remark"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on archive audit", name)
		}
	}
}

func TestArchiveAuditStatus(t *testing.T) {
	tests := []struct {
		action string
		want   int
		ok     bool
	}{
		{"pass", 1, true},
		{"reject", 2, true},
		{"cancel", 3, true},
		{"approve", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := archiveAuditStatus(tt.action)
		if got != tt.want || ok != tt.ok {
			t.Errorf("archiveAuditStatus(%q) = (%d,%v), want (%d,%v)", tt.action, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBoolFormValue(t *testing.T) {
	if got := boolFormValue(true); got != "true" {
		t.Errorf("boolFormValue(true) = %q, want true", got)
	}
	if got := boolFormValue(false); got != "false" {
		t.Errorf("boolFormValue(false) = %q, want false", got)
	}
}

func TestParseArchiveID(t *testing.T) {
	if id, err := parseArchiveID(" 7 "); err != nil || id != 7 {
		t.Errorf("parseArchiveID(' 7 ') = (%d,%v), want (7,nil)", id, err)
	}
	for _, bad := range []string{"0", "-1", "abc", ""} {
		if _, err := parseArchiveID(bad); err == nil {
			t.Errorf("parseArchiveID(%q) expected error", bad)
		}
	}
}

func TestExtractArchiveTotal(t *testing.T) {
	// Flat {total, rows} document as returned by archive_list / archive_log.
	flat := map[string]any{"total": float64(42), "rows": []any{}}
	if got := extractArchiveTotal(flat); got != 42 {
		t.Errorf("extractArchiveTotal(flat) = %d, want 42", got)
	}
	// Nested fallback shape.
	nested := map[string]any{"data": map[string]any{"total": float64(5)}}
	if got := extractArchiveTotal(nested); got != 5 {
		t.Errorf("extractArchiveTotal(nested) = %d, want 5", got)
	}
	if got := extractArchiveTotal("not a map"); got != 0 {
		t.Errorf("extractArchiveTotal(non-map) = %d, want 0", got)
	}
}

func TestExtractArchiveItemsFlat(t *testing.T) {
	raw := map[string]any{
		"total": float64(1),
		"rows": []any{
			map[string]any{"id": float64(7), "title": "t", "src_db_name": "app"},
		},
	}
	items := extractArchiveItems(raw)
	if len(items) != 1 {
		t.Fatalf("extractArchiveItems len = %d, want 1", len(items))
	}
	if items[0]["src_db_name"] != "app" {
		t.Errorf("row src_db_name = %v, want app", items[0]["src_db_name"])
	}
}
