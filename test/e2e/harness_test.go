package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	binaryPath string
	mockURL    string
)

// universalBody satisfies the common Archery response shapes: JWT login,
// DRF-style list endpoints (count/results), and internal Django endpoints
// (status/msg/data with total/rows lists). Path-specific overrides in
// universalMux below handle parsers that need a different shape.
const universalBody = `{
  "access": "test-access-token",
  "refresh": "test-refresh-token",
  "status": 0,
  "msg": "ok",
  "data": {"total": 0, "rows": [], "results": [], "count": 0},
  "count": 0,
  "next": null,
  "previous": null,
  "results": [],
  "total": 0,
  "rows": []
}`

// pathOverrides maps a request-path prefix to a canned response for parsers
// the universal body cannot satisfy. Longest prefix wins.
var pathOverrides = map[string]string{
	// explain renders data as a row list
	"/query/explain/": `{"status":0,"msg":"ok","data":[]}`,
	// generate_sql and the optimizers return data as a string payload
	"/query/generate_sql/": `{"status":0,"msg":"ok","data":"SELECT COUNT(*) FROM t1"}`,
	"/slowquery/optimize_": `{"status":0,"msg":"ok","data":"-- advice: add an index"}`,
}

func universalMux() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Django session login flow for internal-mode commands: GET /login/
		// primes the csrftoken cookie, POST /authenticate/ sets sessionid.
		switch {
		case r.URL.Path == "/login/" && r.Method == http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "test-csrf", Path: "/"})
			w.WriteHeader(http.StatusOK)
			return
		case r.URL.Path == "/authenticate/" && r.Method == http.MethodPost:
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "test-session", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		best := ""
		for prefix := range pathOverrides {
			if strings.HasPrefix(r.URL.Path, prefix) && len(prefix) > len(best) {
				best = prefix
			}
		}
		if best != "" {
			_, _ = w.Write([]byte(pathOverrides[best]))
			return
		}
		_, _ = w.Write([]byte(universalBody))
	})
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "archery-e2e-*")
	if err != nil {
		panic("temp dir: " + err.Error())
	}

	name := "archery-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binaryPath = filepath.Join(dir, name)

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/archery-cli")
	build.Dir = projectRoot()
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build binary: " + err.Error())
	}

	server := httptest.NewServer(universalMux())
	mockURL = server.URL

	code := m.Run()
	server.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func projectRoot() string {
	_, src, _, _ := runtime.Caller(0)
	dir := filepath.Dir(src)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	wd, _ := os.Getwd()
	return wd
}

type cmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type jsonEnvelope struct {
	OK            bool            `json:"ok"`
	SchemaVersion string          `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
	Error         *jsonError      `json:"error"`
	Meta          map[string]any  `json:"meta"`
}

type jsonError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	Retryable bool           `json:"retryable"`
}

// runCLI executes the built binary against the universal mock. home isolates
// the config directory (confirm.secret, update cache) and must be shared
// between a dry-run and its confirm step.
func runCLI(home string, extraEnv map[string]string, args ...string) cmdResult {
	cmd := exec.Command(binaryPath, args...)
	env := []string{}
	drop := map[string]bool{
		"ARCHERY_CLI_URL": true, "ARCHERY_CLI_USERNAME": true,
		"ARCHERY_CLI_PASSWORD": true, "ARCHERY_CLI_REGION": true,
		"HOME": true, "USERPROFILE": true,
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			env = append(env, kv)
		}
	}
	env = append(env,
		"ARCHERY_CLI_URL="+mockURL,
		"ARCHERY_CLI_USERNAME=tester",
		"ARCHERY_CLI_PASSWORD=secret",
		"ARCHERY_CLI_NO_UPDATE_CHECK=1",
		"HOME="+home,
		"USERPROFILE="+home,
	)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func decodeEnvelope(t *testing.T, stdout string) jsonEnvelope {
	t.Helper()
	var env jsonEnvelope
	trimmed := strings.TrimSpace(stdout)
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\nstdout: %s", err, stdout)
	}
	if env.SchemaVersion == "" {
		t.Fatalf("missing schema_version: %s", stdout)
	}
	return env
}

func decodeOK(t *testing.T, r cmdResult) map[string]any {
	t.Helper()
	if r.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	env := decodeEnvelope(t, r.Stdout)
	if !env.OK {
		t.Fatalf("ok=false: %s", r.Stdout)
	}
	var data map[string]any
	if len(env.Data) > 0 {
		// data may be an array for list commands; tolerate both.
		if err := json.Unmarshal(env.Data, &data); err != nil {
			var arr []any
			if err2 := json.Unmarshal(env.Data, &arr); err2 != nil {
				t.Fatalf("envelope data is neither object nor array: %v\n%s", err, r.Stdout)
			}
		}
	}
	return data
}
