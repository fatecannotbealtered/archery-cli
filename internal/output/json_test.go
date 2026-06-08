package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func captureStdoutStderr(fn func()) (stdout, stderr string) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr
	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(rOut)
	_, _ = errBuf.ReadFrom(rErr)
	return outBuf.String(), errBuf.String()
}

func TestPrintJSON(t *testing.T) {
	origCompact := Compact
	defer func() { Compact = origCompact }()
	Compact = false

	stdout, stderr := captureStdoutStderr(func() {
		PrintJSON(map[string]any{"key": "value"})
	})
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}

	var env SuccessEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, stdout)
	}
	if !env.OK {
		t.Error("expected ok=true")
	}
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %q, want %q", env.SchemaVersion, SchemaVersion)
	}
	if env.Data == nil {
		t.Error("expected non-nil data")
	}
}

func TestPrintJSON_ExitOnError(t *testing.T) {
	if os.Getenv("TEST_PRINTJSON_EXIT") == "1" {
		PrintJSON(make(chan int))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrintJSON_ExitOnError$", "-test.count=1")
	cmd.Env = append(os.Environ(), "TEST_PRINTJSON_EXIT=1")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exit error, got %v", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.ExitCode())
	}
}

func TestPrintJSONErr_SuccessAndError(t *testing.T) {
	origCompact := Compact
	defer func() { Compact = origCompact }()
	Compact = false

	stdout, stderr := captureStdoutStderr(func() {
		if err := PrintJSONErr(map[string]any{"a": 1}); err != nil {
			t.Errorf("PrintJSONErr success: %v", err)
		}
	})
	if !strings.Contains(stdout, `"a"`) {
		t.Errorf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}

	_, stderr = captureStdoutStderr(func() {
		err := PrintJSONErr(make(chan int))
		if err == nil {
			t.Error("expected marshal error")
		}
	})
	if !strings.Contains(stderr, "json marshal error") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestPrintErrorJSON(t *testing.T) {
	origCompact := Compact
	defer func() { Compact = origCompact }()
	Compact = false

	// emitErrorPayload writes to stderr in archery-cli.
	_, stderr := captureStdoutStderr(func() {
		PrintErrorJSON("not found", 404)
		PrintErrorJSON("local", 0)
	})
	if !strings.Contains(stderr, `"ok": false`) || !strings.Contains(stderr, `"code": "E_NOT_FOUND"`) {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, `"code": "E_UNKNOWN"`) {
		t.Errorf("status 0 stderr = %q", stderr)
	}
}

func TestPrintErrorJSONWithCode(t *testing.T) {
	origCompact := Compact
	defer func() { Compact = origCompact }()
	Compact = false

	_, stderr := captureStdoutStderr(func() {
		PrintErrorJSONWithCode("rate limited", 429, E_RATE_LIMIT)
	})
	if stderr == "" {
		t.Fatal("expected error JSON on stderr")
	}
	if !strings.Contains(stderr, `"ok": false`) {
		t.Errorf("stderr missing ok=false: %q", stderr)
	}
	if !strings.Contains(stderr, `"code": "E_RATE_LIMIT"`) {
		t.Errorf("stderr missing code: %q", stderr)
	}
	if !strings.Contains(stderr, `"message": "rate limited"`) {
		t.Errorf("stderr missing message: %q", stderr)
	}
	if !strings.Contains(stderr, `"retryable": true`) {
		t.Errorf("E_RATE_LIMIT should be retryable: %q", stderr)
	}

	var env ErrorEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("invalid JSON on stderr: %v\nraw: %s", err, stderr)
	}
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %q, want %q", env.SchemaVersion, SchemaVersion)
	}
	if env.Error.Details == nil {
		t.Error("expected non-nil details")
	}
}

func TestRetryableErrorCode(t *testing.T) {
	retryable := []ErrorCode{E_RATE_LIMIT, E_SERVER, E_NETWORK, E_TIMEOUT}
	for _, code := range retryable {
		if !RetryableErrorCode(code) {
			t.Errorf("RetryableErrorCode(%s) = false, want true", code)
		}
	}

	nonRetryable := []ErrorCode{E_CONFIG, E_AUTH, E_FORBIDDEN, E_NOT_FOUND, E_USAGE, E_VALIDATION, E_CONFIRMATION_REQUIRED, E_CONFLICT, E_UNKNOWN}
	for _, code := range nonRetryable {
		if RetryableErrorCode(code) {
			t.Errorf("RetryableErrorCode(%s) = true, want false", code)
		}
	}
}

func TestHintForErrorCode(t *testing.T) {
	allCodes := []ErrorCode{
		E_CONFIG, E_AUTH, E_FORBIDDEN, E_NOT_FOUND, E_USAGE,
		E_VALIDATION, E_CONFIRMATION_REQUIRED, E_CONFLICT, E_RATE_LIMIT,
		E_SERVER, E_NETWORK, E_TIMEOUT,
	}
	for _, code := range allCodes {
		hint := HintForErrorCode(code)
		if hint == "" {
			t.Errorf("HintForErrorCode(%s) returned empty string", code)
		}
	}

	unknown := HintForErrorCode(ErrorCode("NONEXISTENT"))
	if unknown != "" {
		t.Errorf("unknown code hint = %q, want empty", unknown)
	}
}

func TestCompactJSON(t *testing.T) {
	origCompact := Compact
	defer func() { Compact = origCompact }()

	Compact = true
	stdout, _ := captureStdoutStderr(func() {
		PrintJSON(map[string]any{"a": 1})
	})
	out := strings.TrimSpace(stdout)
	if strings.Contains(out, "\n") {
		t.Errorf("expected single-line compact JSON, got:\n%s", out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(out, `"a":1`) {
		t.Errorf("compact JSON missing key: %q", out)
	}
}

func TestEmitErrorPayload_Fallback(t *testing.T) {
	orig := emitJSONMarshal
	emitJSONMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { emitJSONMarshal = orig }()

	_, stderr := captureStdoutStderr(func() {
		emitErrorPayload("fail", 500, E_SERVER)
	})
	if !strings.Contains(stderr, `"code":"E_SERVER"`) {
		t.Errorf("fallback stderr = %q", stderr)
	}
}
