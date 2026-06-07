package output

import (
	"strings"
	"testing"
)

func TestFilterMap_NoFilter_ReturnsAll(t *testing.T) {
	m := map[string]any{"a": 1, "b": 2}
	out := FilterMap(m, nil)
	if len(out) != 2 {
		t.Errorf("expected 2 keys, got %d", len(out))
	}
}

func TestFilterMap_SelectsCaseInsensitive(t *testing.T) {
	m := map[string]any{"id": 1, "name": "alice", "email": "a@x"}
	out := FilterMap(m, []string{"ID", "Email"})
	if len(out) != 2 {
		t.Errorf("expected 2 keys, got %d (%v)", len(out), out)
	}
	if out["id"] != 1 {
		t.Errorf("id = %v, want 1", out["id"])
	}
	if out["email"] != "a@x" {
		t.Errorf("email = %v, want a@x", out["email"])
	}
}

func TestFilterMap_IgnoresUnknown(t *testing.T) {
	m := map[string]any{"id": 1}
	out := FilterMap(m, []string{"id", "not-a-field"})
	if len(out) != 1 {
		t.Errorf("expected 1 key, got %v", out)
	}
}

func TestWorkflowToMap_OmitsEmpty(t *testing.T) {
	f := FlatWorkflow{ID: 1, Name: "deploy"}
	m := WorkflowToMap(f)
	if _, ok := m["description"]; ok {
		t.Error("description should be omitted when empty")
	}
	if _, ok := m["status"]; ok {
		t.Error("status should be omitted when empty")
	}
	if _, ok := m["webUrl"]; ok {
		t.Error("webUrl should be omitted when empty")
	}
	if m["name"] != "deploy" {
		t.Errorf("name = %v, want deploy", m["name"])
	}
}

func TestErrorCodeFromStatus(t *testing.T) {
	cases := map[int]ErrorCode{
		401: E_AUTH,
		403: E_FORBIDDEN,
		404: E_NOT_FOUND,
		409: E_CONFLICT,
		429: E_RATE_LIMIT,
		500: E_SERVER,
		503: E_SERVER,
		400: E_BAD_ARGS,
		422: E_BAD_ARGS,
		0:   E_UNKNOWN,
	}
	for status, want := range cases {
		got := ErrorCodeFromStatus(status)
		if status == 0 {
			if got != E_UNKNOWN {
				t.Errorf("ErrorCodeFromStatus(0) = %v, want %v", got, want)
			}
		} else {
			if got != want {
				t.Errorf("ErrorCodeFromStatus(%d) = %v, want %v", status, got, want)
			}
		}
	}
}

func TestHintForErrorCode_AllCodesHaveHint(t *testing.T) {
	codes := []ErrorCode{
		E_CONFIG, E_AUTH, E_FORBIDDEN, E_NOT_FOUND, E_BAD_ARGS,
		E_VALIDATION, E_CONFIRM_REQUIRED, E_CONFLICT, E_RATE_LIMIT,
		E_SERVER, E_NETWORK, E_TIMEOUT,
	}
	for _, c := range codes {
		if HintForErrorCode(c) == "" {
			t.Errorf("HintForErrorCode(%v) returned empty hint", c)
		}
	}
}

func TestStripAnsi(t *testing.T) {
	in := "\033[31mhello\033[0m"
	out := stripAnsi(in)
	if out != "hello" {
		t.Errorf("stripAnsi = %q, want hello", out)
	}
}

func TestRuneWidth_CJK(t *testing.T) {
	if runeWidth("中文") != 4 {
		t.Errorf("CJK width should be 4, got %d", runeWidth("中文"))
	}
	if runeWidth("abc") != 3 {
		t.Errorf("ASCII width should be 3, got %d", runeWidth("abc"))
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 5); !strings.HasSuffix(got, "…") {
		t.Errorf("truncate should end with ellipsis, got %q", got)
	}
	if got := truncate("ok", 5); got != "ok" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
}

func TestStatusBadge_AllStates(t *testing.T) {
	for _, s := range []string{
		"running", "pending", "in_progress", "open",
		"success", "closed", "merged", "approved", "resolved",
		"failed", "canceled", "rejected", "declined",
		"skipped", "manual", "scheduled", "draft",
		"locked", "created",
		"unknown_state",
	} {
		_ = StatusBadge(s)
		_ = StatusBadge(strings.ToUpper(s))
	}
}
