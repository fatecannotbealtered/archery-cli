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
	// explain renders data as a {column_list, rows} table (v1.8.5 shape)
	"/query/explain/": `{"status":0,"msg":"ok","data":{"column_list":["id","select_type"],"rows":[]}}`,
	// generate_sql and the optimizers return data as a string payload
	"/query/generate_sql/": `{"status":0,"msg":"ok","data":"SELECT COUNT(*) FROM t1"}`,
	"/slowquery/optimize_": `{"status":0,"msg":"ok","data":"-- advice: add an index"}`,
	// binlog list returns data as a row list (one map per binary log)
	"/binlog/list/": `{"status":0,"msg":"ok","data":[]}`,
	// my2sql returns data as a list of {sql, binlog_info} rows
	"/binlog/my2sql/": `{"status":0,"msg":"ok","data":[]}`,
	// data dictionary: table_list returns a letter-keyed map of [name, comment]
	// pairs; table_info returns {column_list, rows} tables (v1.8.5 shapes).
	"/data_dictionary/table_list/": `{"status":0,"data":{"u":[["users","app users"]],"o":[["orders",""]]}}`,
	"/data_dictionary/table_info/": `{"status":0,"data":{` +
		`"meta_data":{"column_list":["table_name","engine"],"rows":["users","InnoDB"]},` +
		`"desc":{"column_list":["col_name","col_type"],"rows":[["id","bigint"],["name","varchar(64)"]]},` +
		`"index":{"column_list":["col_name","index_name"],"rows":[["id","PRIMARY"]]},` +
		`"create_sql":[["users","CREATE TABLE users (...)"]]}}`,
	// instance list (session) returns {total,rows}; detail filters this by id, so
	// row id=1 must be present for `instance detail 1` to resolve.
	"/instance/list/": `{"total":1,"rows":[{"id":1,"instance_name":"inst1","db_type":"mysql","type":"master","host":"127.0.0.1","port":3306,"user":"root"}]}`,
	// instance_resource (session) returns data as a flat name list.
	"/instance/instance_resource/": `{"status":0,"msg":"ok","data":["db1","db2"]}`,
	// describetable (session) returns a ResultSet with positional rows.
	"/instance/describetable/": `{"status":0,"msg":"ok","data":{"column_list":["Table","Create Table"],"rows":[["t1","CREATE TABLE t1 (...)"]],"error":null}}`,
	// instance user list (session) carries rows at the top level.
	"/instance/user/list": `{"status":0,"msg":"ok","rows":[]}`,
	// grant (session) returns the executed SQL in data.
	"/instance/user/grant/": `{"status":0,"msg":"ok","data":"GRANT SELECT ON *.* TO ` + "`app`@`%`" + `;"}`,
	// user.lists returns data as a flat array of user rows (not a {total,rows}
	// object); ids render as strings (bigint_as_string=True).
	"/user/list/": `{"status":0,"msg":"ok","data":[` +
		`{"id":"1","username":"alice","display":"Alice","email":"a@x.io","is_active":true,"is_staff":true,"is_superuser":true},` +
		`{"id":"2","username":"bob","display":"Bob","email":"b@x.io","is_active":true,"is_staff":false,"is_superuser":false}]}`,
	// resource_group.group returns {total, rows:[{group_id, group_name}]}.
	"/group/group/": `{"total":1,"rows":[{"group_id":"1","group_name":"ops","ding_webhook":""}]}`,
	// workflow detail_content returns the execution/review result rows (the data
	// `workflow detail` surfaces as result[], including any execution error).
	"/sqlworkflow/detail_content/": `{"rows":[` +
		`{"id":1,"stage":"Executed","errlevel":2,"stagestatus":"异常终止","errormessage":"Invalid remote backup information","sql":"alter table t add c int","affected_rows":0,"execute_time":"0.000"}]}`,
	// workflow list (session) supplies the detail's metadata (title/status/
	// instance/group), matched by id; row id=1 must be present for `detail 1`.
	"/sqlworkflow_list/": `{"total":1,"rows":[` +
		`{"id":"1","workflow_name":"add column c","status":"workflow_exception","engineer_display":"alice",` +
		`"instance__instance_name":"inst1","db_name":"db1","group_name":"ops","is_backup":true,"syntax_type":1,"create_time":"2026-06-15 10:00:00"}]}`,
	// resource_group.addrelation returns {status, msg}.
	"/group/addrelation/": `{"status":0,"msg":"ok"}`,
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
		// Don't probe the real OS keyring from the spawned binary: a locked
		// macOS Keychain on CI runners blocks the probe and hangs the suite.
		"ARCHERY_CLI_NO_KEYRING=1",
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
