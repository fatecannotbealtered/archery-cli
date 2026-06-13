package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReader) Close() error             { return nil }

var testCtx = context.Background()

func newTestClient(serverURL string) *Client {
	c := NewClient(serverURL)
	c.SetTokens("test-token", "test-refresh")
	// Internal-mode mechanics tests exercise internalRequest directly; mark the
	// Django session as already established so ensureSession is a no-op.
	c.sessionReady = true
	return c
}

// ─── TestNewClient ────────────────────────────────────────────────────────────

func TestNewClient_StripsTrailingSlash(t *testing.T) {
	c := NewClient("https://archery.example.com/")
	if c.host != "https://archery.example.com" {
		t.Errorf("host = %q, want without trailing slash", c.host)
	}
}

func TestNewClient_NoTokenByDefault(t *testing.T) {
	c := NewClient("https://archery.example.com")
	if c.accessToken != "" {
		t.Errorf("accessToken = %q, want empty", c.accessToken)
	}
	if c.refreshToken != "" {
		t.Errorf("refreshToken = %q, want empty", c.refreshToken)
	}
}

func TestNewClient_InitializesSubAPIs(t *testing.T) {
	c := NewClient("https://archery.example.com")
	if c.Auth == nil {
		t.Error("Auth sub-API not initialized")
	}
	if c.Workflows == nil {
		t.Error("Workflows sub-API not initialized")
	}
}

func TestNewClient_SetTokens(t *testing.T) {
	c := NewClient("https://archery.example.com")
	c.SetTokens("access", "refresh")
	if c.accessToken != "access" {
		t.Errorf("accessToken = %q, want access", c.accessToken)
	}
	if c.RefreshToken() != "refresh" {
		t.Errorf("RefreshToken() = %q, want refresh", c.RefreshToken())
	}
}

// ─── TestHost ─────────────────────────────────────────────────────────────────

func TestHost(t *testing.T) {
	c := NewClient("https://archery.example.com")
	if c.Host() != "https://archery.example.com" {
		t.Errorf("Host() = %q", c.Host())
	}
}

// ─── TestAPIPath ──────────────────────────────────────────────────────────────

func TestAPIPath(t *testing.T) {
	c := NewClient("https://archery.example.com")
	cases := map[string]string{
		"/user":      "/api/user",
		"user":       "/api/user",
		"/workflows": "/api/workflows",
		"workflows":  "/api/workflows",
	}
	for in, want := range cases {
		if got := c.APIPath(in); got != want {
			t.Errorf("APIPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// ─── TestDefaultUserAgent ─────────────────────────────────────────────────────

func TestDefaultUserAgent_Default(t *testing.T) {
	t.Setenv("ARCHERY_CLI_USER_AGENT", "")
	if got := defaultUserAgent(); got != "archery-cli" {
		t.Errorf("defaultUserAgent() = %q, want archery-cli", got)
	}
}

func TestDefaultUserAgent_EnvOverride(t *testing.T) {
	t.Setenv("ARCHERY_CLI_USER_AGENT", "custom-agent/1.0")
	if got := defaultUserAgent(); got != "custom-agent/1.0" {
		t.Errorf("defaultUserAgent() = %q, want custom-agent/1.0", got)
	}
}

// ─── TestAPIError_Error ───────────────────────────────────────────────────────

func TestAPIError_Error(t *testing.T) {
	msgErr := &APIError{StatusCode: 400, ErrorMessages: []string{"bad request"}}
	if !strings.Contains(msgErr.Error(), "bad request") {
		t.Errorf("Error() = %q", msgErr.Error())
	}

	fieldErr := &APIError{StatusCode: 422, Errors: map[string]string{"name": "taken"}}
	if !strings.Contains(fieldErr.Error(), "name") {
		t.Errorf("Error() = %q", fieldErr.Error())
	}

	plain := &APIError{StatusCode: 500}
	if plain.Error() != "Archery API error 500" {
		t.Errorf("Error() = %q", plain.Error())
	}
}

// ─── TestMaxRetries ───────────────────────────────────────────────────────────

func TestMaxRetries_Default(t *testing.T) {
	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "")
	if got := maxRetries(); got != defaultMaxRetries {
		t.Errorf("maxRetries() = %d, want default %d", got, defaultMaxRetries)
	}
}

func TestMaxRetries_EnvOverride(t *testing.T) {
	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "1")
	if got := maxRetries(); got != 1 {
		t.Errorf("maxRetries() = %d, want 1", got)
	}

	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "not-a-number")
	if got := maxRetries(); got != defaultMaxRetries {
		t.Errorf("maxRetries() invalid env = %d, want default %d", got, defaultMaxRetries)
	}

	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "-1")
	if got := maxRetries(); got != defaultMaxRetries {
		t.Errorf("maxRetries() negative env = %d, want default %d", got, defaultMaxRetries)
	}
}

// ─── TestIsRetryableNetworkErr ────────────────────────────────────────────────

func TestIsRetryableNetworkErr(t *testing.T) {
	if isRetryableNetworkErr(nil) {
		t.Error("nil error should not be retryable")
	}
	cases := []string{
		"connection reset by peer",
		"connection refused",
		"i/o timeout",
		"temporary failure in name resolution",
		"no such host",
		"tls handshake timeout",
	}
	for _, msg := range cases {
		if !isRetryableNetworkErr(errors.New(msg)) {
			t.Errorf("expected retryable: %q", msg)
		}
	}
	if isRetryableNetworkErr(errors.New("something else entirely")) {
		t.Error("unexpected retryable error")
	}
}

// ─── TestRateLimitWait ────────────────────────────────────────────────────────

func TestRateLimitWait_DefaultAndCap(t *testing.T) {
	wait := rateLimitWait(http.Header{})
	if wait != time.Second {
		t.Errorf("default wait = %v, want 1s", wait)
	}

	h := http.Header{}
	h.Set("Retry-After", "9999")
	if got := rateLimitWait(h); got != maxRateLimitWait {
		t.Errorf("capped wait = %v, want %v", got, maxRateLimitWait)
	}

	h2 := http.Header{}
	h2.Set("Retry-After", "not-a-number")
	if got := rateLimitWait(h2); got != time.Second {
		t.Errorf("invalid Retry-After wait = %v, want 1s", got)
	}
}

// ─── TestWaitForRetry ─────────────────────────────────────────────────────────

func TestWaitForRetry_NonRetryStatus(t *testing.T) {
	if err := waitForRetry(testCtx, 0, http.StatusBadRequest, nil); err != nil {
		t.Errorf("expected nil for 4xx, got %v", err)
	}
}

func TestWaitForRetry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForRetry(ctx, 0, http.StatusInternalServerError, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ─── TestDoWithRetry ──────────────────────────────────────────────────────────

func TestDoWithRetry_429_RespectsRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"msg":"rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	data, err := c.Get(testCtx, c.APIPath("/test"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("body = %s, want contains 'ok'", string(data))
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestDoWithRetry_5xx_ExponentialBackoff(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"msg":"500"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	data, err := c.Get(testCtx, c.APIPath("/test"))
	if err != nil {
		t.Fatalf("Get after retries: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("body = %s, want contains 'ok'", string(data))
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

func TestDoWithRetry_4xx_NoRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"bad request"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", attempts.Load())
	}
}

func TestDoWithRetry_MaxRetriesExhausted(t *testing.T) {
	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "1")
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"msg":"boom"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestDoWithRetry_4xx_Unauthorized_NoRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"msg":"not authenticated"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 401)", attempts.Load())
	}
}

func TestDoWithRetry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newTestClient("http://example.com")
	_, err := c.Get(ctx, c.APIPath("/test"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDoWithRetry_WaitCanceledOn429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"msg":"slow down"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	c := newTestClient(srv.URL)
	_, err := c.Get(ctx, c.APIPath("/test"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 before cancel", attempts.Load())
	}
}

func TestDoWithRetry_WaitCanceledOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"msg":"500"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	c := newTestClient(srv.URL)
	_, err := c.Get(ctx, c.APIPath("/test"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDoWithRetry_NetworkErrorRetry(t *testing.T) {
	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "2")
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	base := http.DefaultTransport
	var transportCalls atomic.Int32
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if transportCalls.Add(1) == 1 {
			return nil, errors.New("connection reset by peer")
		}
		return base.RoundTrip(req)
	})

	data, err := c.Get(testCtx, c.APIPath("/test"))
	if err != nil {
		t.Fatalf("Get after network retry: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("body = %s", string(data))
	}
}

func TestDoWithRetry_NonRetryableNetworkError(t *testing.T) {
	c := newTestClient("http://example.com")
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("certificate verify failed")
	})
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "executing request") {
		t.Errorf("err = %v", err)
	}
}

func TestDoWithRetry_NetworkRetriesExhausted(t *testing.T) {
	t.Setenv("ARCHERY_CLI_MAX_RETRIES", "0")
	c := newTestClient("http://example.com")
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset by peer")
	})
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil || !strings.Contains(err.Error(), "executing request") {
		t.Fatalf("expected executing request error, got %v", err)
	}
}

func TestDoWithRetry_JSONMarshalError(t *testing.T) {
	c := newTestClient("http://example.com")
	_, err := c.Post(testCtx, c.APIPath("/test"), make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "encoding request body") {
		t.Errorf("err = %v", err)
	}
}

func TestDoWithRetry_ReadBodyError(t *testing.T) {
	c := newTestClient("http://example.com")
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReader{},
			Header:     make(http.Header),
		}, nil
	})
	_, err := c.Get(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "reading response body") {
		t.Errorf("err = %v", err)
	}
}

// ─── TestParseError ───────────────────────────────────────────────────────────

func TestParseError_DRFDetail(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(404, []byte(`{"detail":"Not found."}`))
	if len(e.ErrorMessages) != 1 || e.ErrorMessages[0] != "Not found." {
		t.Errorf("DRF detail: %+v", e)
	}
	if e.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", e.StatusCode)
	}
}

func TestParseError_DRFPerFieldValidation(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(400, []byte(`{"name":["has already been taken"],"path":["is invalid"]}`))
	if len(e.Errors) != 2 {
		t.Errorf("Errors = %v, want 2 entries", e.Errors)
	}
	if e.Errors["name"] != "has already been taken" {
		t.Errorf("Errors[name] = %q", e.Errors["name"])
	}
	if e.Errors["path"] != "is invalid" {
		t.Errorf("Errors[path] = %q", e.Errors["path"])
	}
}

func TestParseError_ArcheryInternalFormat(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(200, []byte(`{"status":1,"msg":"query failed","data":null}`))
	if len(e.ErrorMessages) == 0 || e.ErrorMessages[0] != "query failed" {
		t.Errorf("Archery internal msg: %+v", e)
	}
}

func TestParseError_ArcheryInternalWithDRFDetail(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(400, []byte(`{"detail":"invalid","status":1,"msg":"also bad","data":null}`))
	if len(e.ErrorMessages) != 2 || e.ErrorMessages[0] != "invalid" || e.ErrorMessages[1] != "also bad" {
		t.Errorf("combined: %+v", e)
	}
}

func TestParseError_FallbackUnauthorized(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(401, []byte(`{}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "not logged in") {
		t.Errorf("401 fallback: %+v", e)
	}
}

func TestParseError_FallbackForbidden(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(403, []byte(`{}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "permission denied") {
		t.Errorf("403 fallback: %+v", e)
	}
}

func TestParseError_FallbackNotFound(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(404, []byte(`{}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "resource not found") {
		t.Errorf("404 fallback: %+v", e)
	}
}

func TestParseError_FallbackUnexpectedStatus(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(418, []byte(`{}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "unexpected status code 418") {
		t.Errorf("unexpected status fallback: %+v", e)
	}
}

func TestParseError_EmptyBody(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(500, []byte{})
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "unexpected status code 500") {
		t.Errorf("empty body fallback: %+v", e)
	}
}

func TestParseError_InvalidJSON(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(400, []byte(`not json`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "unexpected status code 400") {
		t.Errorf("invalid JSON fallback: %+v", e)
	}
}

func TestParseError_ArcheryInternalEmptyMsg(t *testing.T) {
	c := NewClient("http://example.com")
	e := c.parseError(200, []byte(`{"status":1,"msg":"","data":null}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "unexpected status code 200") {
		t.Errorf("empty msg fallback: %+v", e)
	}
}

func TestParseError_AllBranches(t *testing.T) {
	c := NewClient("http://example.com")

	// DRF detail string
	e := c.parseError(400, []byte(`{"detail":"bad request"}`))
	if len(e.ErrorMessages) != 1 || e.ErrorMessages[0] != "bad request" {
		t.Errorf("string detail: %+v", e)
	}

	// DRF per-field validation
	e = c.parseError(400, []byte(`{"field":["a","b"]}`))
	if e.Errors["field"] != "a; b" {
		t.Errorf("joined validation errors = %q", e.Errors["field"])
	}

	// Archery internal msg
	e = c.parseError(200, []byte(`{"status":1,"msg":"error message","data":null}`))
	if len(e.ErrorMessages) == 0 || e.ErrorMessages[0] != "error message" {
		t.Errorf("archery msg: %+v", e)
	}

	// 401 with msg overrides fallback
	e = c.parseError(401, []byte(`{"status":1,"msg":"custom auth error","data":null}`))
	if len(e.ErrorMessages) == 0 || e.ErrorMessages[0] != "custom auth error" {
		t.Errorf("401 with msg: %+v", e)
	}

	// 418 no parseable fields
	e = c.parseError(418, []byte(`{}`))
	if len(e.ErrorMessages) != 1 || !strings.Contains(e.ErrorMessages[0], "unexpected status code 418") {
		t.Errorf("418 fallback: %+v", e)
	}
}

// ─── TestTagUntrusted ─────────────────────────────────────────────────────────

func TestTagUntrusted_AddsField(t *testing.T) {
	data := map[string]any{
		"name":    "test",
		"content": "user input",
	}
	TagUntrusted(data, "content")
	untrusted, ok := data["_untrusted"].([]string)
	if !ok {
		t.Fatalf("_untrusted not set or wrong type: %T", data["_untrusted"])
	}
	if len(untrusted) != 1 || untrusted[0] != "content" {
		t.Errorf("_untrusted = %v, want [content]", untrusted)
	}
}

func TestTagUntrusted_MultipleFields(t *testing.T) {
	data := map[string]any{
		"name":    "test",
		"content": "user input",
		"body":    "more input",
	}
	TagUntrusted(data, "content", "body")
	untrusted := data["_untrusted"].([]string)
	if len(untrusted) != 2 {
		t.Errorf("_untrusted = %v, want 2 entries", untrusted)
	}
}

func TestTagUntrusted_SkipsMissingFields(t *testing.T) {
	data := map[string]any{
		"name": "test",
	}
	TagUntrusted(data, "name", "missing")
	untrusted := data["_untrusted"].([]string)
	if len(untrusted) != 1 || untrusted[0] != "name" {
		t.Errorf("_untrusted = %v, want [name]", untrusted)
	}
}

func TestTagUntrusted_AllFieldsMissing(t *testing.T) {
	data := map[string]any{
		"name": "test",
	}
	TagUntrusted(data, "missing1", "missing2")
	if _, ok := data["_untrusted"]; ok {
		t.Error("_untrusted should not be set when no fields match")
	}
}

func TestTagUntrusted_EmptyFields(t *testing.T) {
	data := map[string]any{
		"name": "test",
	}
	TagUntrusted(data)
	if _, ok := data["_untrusted"]; ok {
		t.Error("_untrusted should not be set when no fields requested")
	}
}

// ─── TestRestRequest (via Get/Post convenience verbs) ─────────────────────────

func TestRestRequest_SendsBearerToken(t *testing.T) {
	var gotAuth, gotContentType, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Get(testCtx, c.APIPath("/test")); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want 'application/json'", gotAccept)
	}
	if gotContentType != "" {
		t.Errorf("Content-Type on GET = %q, want empty", gotContentType)
	}
}

func TestRestRequest_PostSendsJSON(t *testing.T) {
	var gotAuth, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Post(testCtx, c.APIPath("/test"), map[string]string{"key": "value"}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", gotContentType)
	}
}

func TestRestRequest_NoTokenWhenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	// No tokens set
	if _, err := c.Get(testCtx, c.APIPath("/test")); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty when no token set", gotAuth)
	}
}

// ─── TestInternalRequest ──────────────────────────────────────────────────────

func TestInternalRequest_SendsFormHeaders(t *testing.T) {
	var gotAuth, gotContentType, gotXHR string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotXHR = r.Header.Get("X-Requested-With")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	form := url.Values{}
	form.Set("key", "value")
	if _, err := c.InternalPost(testCtx, c.APIPath("/test"), form); err != nil {
		t.Fatalf("InternalPost: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", gotAuth)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want 'application/x-www-form-urlencoded'", gotContentType)
	}
	if gotXHR != "XMLHttpRequest" {
		t.Errorf("X-Requested-With = %q, want 'XMLHttpRequest'", gotXHR)
	}
}

func TestInternalRequest_FormEncoded(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	form := url.Values{}
	form.Set("sql", "SELECT 1")
	form.Set("env", "production")
	if _, err := c.InternalPost(testCtx, c.APIPath("/query"), form); err != nil {
		t.Fatalf("InternalPost: %v", err)
	}
	if !strings.Contains(gotBody, "sql=SELECT+1") {
		t.Errorf("body = %q, missing form-encoded sql", gotBody)
	}
	if !strings.Contains(gotBody, "env=production") {
		t.Errorf("body = %q, missing form-encoded env", gotBody)
	}
}

func TestInternalRequest_GetNoBody(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.InternalGet(testCtx, c.APIPath("/test")); err != nil {
		t.Fatalf("InternalGet: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

func TestInternalRequest_RetryOn5xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"msg":"500"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	data, err := c.InternalGet(testCtx, c.APIPath("/test"))
	if err != nil {
		t.Fatalf("InternalGet: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("body = %s", string(data))
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestInternalRequest_4xx_NoRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"msg":"forbidden"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.InternalGet(testCtx, c.APIPath("/test"))
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", attempts.Load())
	}
}

// ─── TestHTTPClient ───────────────────────────────────────────────────────────

func TestHTTPClient_CheckRedirect_SameHostPreservesAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/start":
			http.Redirect(w, r, "/api/end", http.StatusFound)
		case "/api/end":
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Get(testCtx, c.APIPath("/start")); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization after same-host redirect = %q", gotAuth)
	}
}

func TestHTTPClient_CheckRedirect_TooManyRedirects(t *testing.T) {
	hc := newHTTPClient(ClientOptions{Timeout: time.Second})
	via := make([]*http.Request, 10)
	err := hc.CheckRedirect(httptest.NewRequest(http.MethodGet, "http://example.com/x", nil), via)
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}

func TestDefaultUserAgent_UsesArcheryCLI(t *testing.T) {
	t.Setenv("ARCHERY_CLI_USER_AGENT", "")
	got := defaultUserAgent()
	if got != "archery-cli" {
		t.Errorf("defaultUserAgent() = %q, want archery-cli", got)
	}
}

func TestEnvUserAgentOverride(t *testing.T) {
	t.Setenv("ARCHERY_CLI_USER_AGENT", "my-bot/2.0")
	got := defaultUserAgent()
	if got != "my-bot/2.0" {
		t.Errorf("defaultUserAgent() = %q, want my-bot/2.0", got)
	}
}

func TestCookieJar_Available(t *testing.T) {
	c := NewClient("http://example.com")
	if c.CookieJar() == nil {
		t.Error("CookieJar() should not be nil")
	}
}

// ─── TestExtractPagination ───────────────────────────────────────────────────

func TestExtractPagination_EmptyHeaders(t *testing.T) {
	p := extractPagination(http.Header{})
	if p.Count != 0 || p.Page != 0 || p.PerPage != 0 || p.NextPage != 0 || p.PrevPage != 0 {
		t.Errorf("expected all zeros for empty headers, got %+v", p)
	}
}

func TestExtractPagination_DRFHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Total-Count", "100")
	h.Set("X-Page", "2")
	h.Set("X-Per-Page", "20")
	h.Set("X-Next-Page", "3")
	h.Set("X-Prev-Page", "1")

	p := extractPagination(h)
	if p.Count != 100 {
		t.Errorf("Count = %d, want 100", p.Count)
	}
	if p.Page != 2 {
		t.Errorf("Page = %d, want 2", p.Page)
	}
	if p.PerPage != 20 {
		t.Errorf("PerPage = %d, want 20", p.PerPage)
	}
	if p.NextPage != 3 {
		t.Errorf("NextPage = %d, want 3", p.NextPage)
	}
	if p.PrevPage != 1 {
		t.Errorf("PrevPage = %d, want 1", p.PrevPage)
	}
}

func TestExtractPagination_LinkHeaderNext(t *testing.T) {
	h := http.Header{}
	h.Set("X-Total-Count", "50")
	h.Set("X-Per-Page", "20")
	h.Set("Link", `<https://archery.example.com/api/v1/workflow/?limit=20&offset=20>; rel="next"`)

	p := extractPagination(h)
	if p.Count != 50 {
		t.Errorf("Count = %d, want 50", p.Count)
	}
	if p.NextPage != 2 {
		t.Errorf("NextPage = %d, want 2 (from Link header offset)", p.NextPage)
	}
}

func TestExtractPagination_LinkHeaderPageParam(t *testing.T) {
	h := http.Header{}
	h.Set("Link", `<https://archery.example.com/api/v1/workflow/?page=3>; rel="next", <https://archery.example.com/api/v1/workflow/?page=1>; rel="prev"`)

	p := extractPagination(h)
	if p.NextPage != 3 {
		t.Errorf("NextPage = %d, want 3", p.NextPage)
	}
	if p.PrevPage != 1 {
		t.Errorf("PrevPage = %d, want 1", p.PrevPage)
	}
}

func TestExtractPagination_InvalidValuesIgnored(t *testing.T) {
	h := http.Header{}
	h.Set("X-Total-Count", "not-a-number")
	h.Set("X-Page", "")

	p := extractPagination(h)
	if p.Count != 0 {
		t.Errorf("Count = %d, want 0 for invalid value", p.Count)
	}
	if p.Page != 0 {
		t.Errorf("Page = %d, want 0 for empty value", p.Page)
	}
}

func TestExtractPagination_LinkHeaderEmpty(t *testing.T) {
	h := http.Header{}
	h.Set("X-Total-Count", "10")
	h.Set("Link", "")

	p := extractPagination(h)
	if p.NextPage != 0 || p.PrevPage != 0 {
		t.Errorf("expected 0 for next/prev with empty Link, got NextPage=%d PrevPage=%d", p.NextPage, p.PrevPage)
	}
}

// ─── TestParseLinkHeader ────────────────────────────────────────────────────

func TestParseLinkHeader_MultipleLinks(t *testing.T) {
	p := Pagination{
		Link: `<https://host/api/v1/workflow/?page=5>; rel="next", <https://host/api/v1/workflow/?page=3>; rel="prev"`,
	}
	parseLinkHeader(&p)
	if p.NextPage != 5 {
		t.Errorf("NextPage = %d, want 5", p.NextPage)
	}
	if p.PrevPage != 3 {
		t.Errorf("PrevPage = %d, want 3", p.PrevPage)
	}
}

func TestParseLinkHeader_OffsetBased(t *testing.T) {
	p := Pagination{
		PerPage: 20,
		Link:    `<https://host/api/v1/workflow/?limit=20&offset=40>; rel="next"`,
	}
	parseLinkHeader(&p)
	if p.NextPage != 3 {
		t.Errorf("NextPage = %d, want 3 (offset 40 / limit 20 + 1)", p.NextPage)
	}
}

func TestParseLinkHeader_NoRel(t *testing.T) {
	p := Pagination{
		Link: `<https://host/api/v1/workflow/?page=2>`,
	}
	parseLinkHeader(&p)
	if p.NextPage != 0 || p.PrevPage != 0 {
		t.Errorf("expected 0 for links without rel, got NextPage=%d PrevPage=%d", p.NextPage, p.PrevPage)
	}
}

// TestEnsureSession_FormLoginFlow verifies internal-mode requests perform the
// Django form login (GET /login/ for csrf, POST /authenticate/) and then send
// the session cookie + X-CSRFToken on the actual request. Regression guard for
// the live-smoke finding that session-mode commands never authenticated.
func TestEnsureSession_FormLoginFlow(t *testing.T) {
	var sawAuthenticate, sawCSRFHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login/" && r.Method == http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf-xyz", Path: "/"})
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/authenticate/" && r.Method == http.MethodPost:
			sawAuthenticate = true
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sess-abc", Path: "/"})
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok"}`))
		default:
			if r.Header.Get("X-CSRFToken") == "csrf-xyz" {
				sawCSRFHeader = true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":0,"msg":"ok","data":{}}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetSessionCredentials("admin", "secret")
	form := url.Values{}
	form.Set("k", "v")
	if _, err := c.InternalPost(testCtx, "/db_diagnostic/process/", form); err != nil {
		t.Fatalf("InternalPost: %v", err)
	}
	if !sawAuthenticate {
		t.Error("expected a POST /authenticate/ session login")
	}
	if !sawCSRFHeader {
		t.Error("expected X-CSRFToken header on the POST internal request")
	}
	if !c.sessionReady {
		t.Error("session should be marked ready after successful login")
	}
}

// TestEnsureSession_NoCredentials returns a clear error when only a JWT is
// present (keyring config) but a session-mode command is invoked.
func TestEnsureSession_NoCredentials(t *testing.T) {
	c := NewClient("http://127.0.0.1:0")
	_, err := c.InternalGet(testCtx, "/data_dictionary/table_list/")
	if err == nil {
		t.Fatal("expected an error when session credentials are absent")
	}
}
