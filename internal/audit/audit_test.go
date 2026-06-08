package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newAuditTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetDirForTest(dir)
	t.Cleanup(func() { SetDirForTest("") })
	return dir
}

func TestLogEntry(t *testing.T) {
	dir := newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_NO_AUDIT", "")

	Log("archery-cli shoot", []string{"shoot", "--target", "bullseye"}, 0, 15)

	files, err := Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 audit file, got %d (in %s)", len(files), dir)
	}

	f, err := os.Open(files[0])
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Scan()
	line := scanner.Bytes()

	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("invalid JSONL line: %v (line=%s)", err, string(line))
	}
	for _, key := range []string{"ts", "cmd", "args", "exit", "ms"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in %v", key, got)
		}
	}
	if got["cmd"] != "archery-cli shoot" {
		t.Errorf("cmd = %v, want archery-cli shoot", got["cmd"])
	}
	if got["exit"].(float64) != 0 {
		t.Errorf("exit = %v, want 0", got["exit"])
	}
	if got["ms"].(float64) != 15 {
		t.Errorf("ms = %v, want 15", got["ms"])
	}
	ts, err := time.Parse(time.RFC3339Nano, got["ts"].(string))
	if err != nil {
		t.Fatalf("ts is not RFC3339Nano: %v", err)
	}
	if ts.Location() != time.UTC {
		t.Errorf("ts location = %v, want UTC", ts.Location())
	}
}

func TestSanitizeArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "two-arg --password",
			in:   []string{"auth", "login", "--password", "secret"},
			want: []string{"auth", "login"},
		},
		{
			name: "combined --password=value",
			in:   []string{"auth", "login", "--password=secret"},
			want: []string{"auth", "login", "--password=***"},
		},
		{
			name: "short -p",
			in:   []string{"auth", "login", "-p", "secret"},
			want: []string{"auth", "login"},
		},
		{
			name: "case-insensitive --PASSWORD=",
			in:   []string{"auth", "login", "--PASSWORD=secret"},
			want: []string{"auth", "login", "--PASSWORD=***"},
		},
		{
			name: "two-arg --token",
			in:   []string{"shoot", "--token", "abc123"},
			want: []string{"shoot"},
		},
		{
			name: "combined --token=value",
			in:   []string{"shoot", "--token=abc123"},
			want: []string{"shoot", "--token=***"},
		},
		{
			name: "short -t",
			in:   []string{"shoot", "-t", "abc123"},
			want: []string{"shoot"},
		},
		{
			name: "two-arg --confirm",
			in:   []string{"shoot", "--confirm", "yes"},
			want: []string{"shoot"},
		},
		{
			name: "combined --confirm=value",
			in:   []string{"shoot", "--confirm=yes"},
			want: []string{"shoot", "--confirm=***"},
		},
		{
			name: "short -c",
			in:   []string{"shoot", "-c", "yes"},
			want: []string{"shoot"},
		},
		{
			name: "non-sensitive untouched",
			in:   []string{"--target", "bullseye", "--distance", "70m"},
			want: []string{"--target", "bullseye", "--distance", "70m"},
		},
		{
			name: "long value redacted",
			in:   []string{"plain", strings.Repeat("x", 257)},
			want: []string{"plain", "[REDACTED len=257]"},
		},
		{
			name: "mixed sensitive and normal",
			in:   []string{"shoot", "--target", "bullseye", "--password", "secret", "--token", "abc"},
			want: []string{"shoot", "--target", "bullseye"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeArgs(tc.in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("sanitizeArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanupOldFiles(t *testing.T) {
	dir := newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_AUDIT_RETENTION_MONTHS", "3")

	oldPath := filepath.Join(dir, "audit-2020-01.jsonl")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0600); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}
	keepPath := filepath.Join(dir, "audit-2099-12.jsonl")
	if err := os.WriteFile(keepPath, []byte("keep\n"), 0600); err != nil {
		t.Fatalf("WriteFile keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0600); err != nil {
		t.Fatalf("WriteFile other: %v", err)
	}

	cleanup(dir)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected old audit file removed, stat err = %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("expected recent audit file kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "other.txt")); err != nil {
		t.Errorf("expected unrelated file kept: %v", err)
	}
}

func TestCleanup_RetentionZeroSkipsDeletion(t *testing.T) {
	dir := newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_AUDIT_RETENTION_MONTHS", "0")

	oldPath := filepath.Join(dir, "audit-2020-01.jsonl")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cleanup(dir)

	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("retention=0 should not delete old files: %v", err)
	}
}

func TestAuditDisabled(t *testing.T) {
	_ = newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_NO_AUDIT", "1")

	Log("anything", []string{"x"}, 0, 1)

	files, _ := Files()
	if len(files) != 0 {
		t.Errorf("expected no files when ARCHERY_CLI_NO_AUDIT=1, got %d", len(files))
	}
}

func TestLog_MarshalError(t *testing.T) {
	_ = newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_NO_AUDIT", "")

	orig := marshalEntry
	marshalEntry = func(any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	t.Cleanup(func() { marshalEntry = orig })

	Log("marshal-fail", []string{"x"}, 1, 2)

	files, err := Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no audit files on marshal error, got %d", len(files))
	}
}

func TestLog_OpenFileError(t *testing.T) {
	dir := newAuditTempDir(t)
	t.Setenv("ARCHERY_CLI_NO_AUDIT", "")

	monthFile := "audit-" + time.Now().Format("2006-01") + ".jsonl"
	if err := os.Mkdir(filepath.Join(dir, monthFile), 0700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	Log("open-fail", []string{"y"}, 2, 3)

	files, err := Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no jsonl files when OpenFile fails, got %v", files)
	}
}
