package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// marshalEntry serializes audit entries; overridden in tests. // test hook
var marshalEntry = json.Marshal

// entry is a single audit log record written as one JSON line.
type entry struct {
	Ts   string   `json:"ts"`
	Cmd  string   `json:"cmd"`
	Args []string `json:"args"`
	Exit int      `json:"exit"`
	Ms   int64    `json:"ms"`
}

// testDir overrides Dir() when set (for testing).
var testDir string

// SetDirForTest overrides the audit directory. Pass an empty string to reset.
// Intended for tests only.
func SetDirForTest(dir string) { testDir = dir }

// Dir returns the audit log directory ~/.archery-cli/audit/
func Dir() string {
	if testDir != "" {
		return testDir
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return filepath.Join(".archery-cli", "audit")
		}
	}
	return filepath.Join(home, ".archery-cli", "audit")
}

// Log writes one audit entry to the current month's JSONL file.
// It is a no-op if ARCHERY_CLI_NO_AUDIT=1. It performs lazy cleanup of old files.
func Log(cmdPath string, args []string, exitCode int, durationMs int64) {
	if os.Getenv("ARCHERY_CLI_NO_AUDIT") == "1" {
		return
	}

	dir := Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}

	cleanup(dir)

	e := entry{
		Ts:   time.Now().Format(time.RFC3339Nano),
		Cmd:  cmdPath,
		Args: sanitizeArgs(args),
		Exit: exitCode,
		Ms:   durationMs,
	}

	data, err := marshalEntry(e)
	if err != nil {
		return
	}
	data = append(data, '\n')

	filename := "audit-" + time.Now().Format("2006-01") + ".jsonl"
	path := filepath.Join(dir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	_, _ = f.Write(data)
}

// retentionMonths returns the number of months to keep audit files.
// Defaults to 3. Set ARCHERY_CLI_AUDIT_RETENTION_MONTHS=0 to disable cleanup.
func retentionMonths() int {
	s := os.Getenv("ARCHERY_CLI_AUDIT_RETENTION_MONTHS")
	if s == "" {
		return 3
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 3
	}
	return n
}

// cleanup removes audit JSONL files older than the retention period.
func cleanup(dir string) {
	months := retentionMonths()
	if months == 0 {
		return
	}

	cutoff := time.Now().AddDate(0, -months, 0).Format("2006-01")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		ym := strings.TrimPrefix(name, "audit-")
		ym = strings.TrimSuffix(ym, ".jsonl")
		if ym < cutoff {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// sensitiveFlags contains flag names whose values must be redacted in audit logs.
var sensitiveFlags = map[string]bool{
	"password": true,
	"p":        true, // short for --password
	"token":    true,
	"t":        true, // short for --token
	"confirm":  true,
	"c":        true, // short for --confirm
}

// sanitizeArgs removes sensitive flag values and returns a clean copy.
// Handles: --password value, --password=value, -p value, -p=value (case-insensitive).
func sanitizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		lower := strings.ToLower(arg)

		if strings.HasPrefix(arg, "-") && strings.Contains(lower, "=") {
			flag := strings.TrimPrefix(strings.TrimPrefix(strings.SplitN(lower, "=", 2)[0], "--"), "-")
			if sensitiveFlags[flag] {
				parts := strings.SplitN(arg, "=", 2)
				out = append(out, parts[0]+"=***")
				continue
			}
		}

		if strings.HasPrefix(arg, "-") {
			stripped := strings.TrimPrefix(lower, "--")
			stripped = strings.TrimPrefix(stripped, "-")
			if sensitiveFlags[stripped] {
				i++ // skip next arg (the value)
				continue
			}
		}

		if !strings.HasPrefix(arg, "-") && len(arg) > 256 {
			out = append(out, "[REDACTED len="+strconv.Itoa(len(arg))+"]")
			continue
		}

		out = append(out, arg)
	}
	return out
}

// Files returns the list of audit JSONL files in the audit directory, sorted by name.
// Exported for testing.
func Files() ([]string, error) {
	dir := Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
