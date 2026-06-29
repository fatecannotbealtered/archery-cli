package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// APIBasePath is the Archery REST API base path.
const APIBasePath = "/api"

const (
	defaultMaxRetries = 3
	maxRateLimitWait  = 60 * time.Second
)

// globalHTTP holds the current client options, set from cmd flags/env before any client is created.
var globalHTTP = ClientOptions{Timeout: 30 * time.Second}

// ClientOptions configures HTTP transport settings.
type ClientOptions struct {
	Timeout            time.Duration
	InsecureSkipVerify bool
}

// SetClientOptions updates global HTTP settings (from cmd flags).
func SetClientOptions(o ClientOptions) {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	globalHTTP = o
}

// CurrentClientOptions returns the global HTTP settings (for tests/diagnostics).
func CurrentClientOptions() ClientOptions {
	return globalHTTP
}

// defaultUserAgent returns ARCHERY_CLI_USER_AGENT if set, otherwise a clear CLI UA.
func defaultUserAgent() string {
	if ua := strings.TrimSpace(os.Getenv("ARCHERY_CLI_USER_AGENT")); ua != "" {
		return ua
	}
	return "archery-cli"
}

func newHTTPClient(opts ClientOptions) *http.Client {
	jar, _ := cookiejar.New(nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &http.Client{
		Timeout:   opts.Timeout,
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// A redirect to the login page means Archery treated the request as
			// unauthenticated (Django @login_required). Do NOT follow it into the
			// HTML login page — return the 3xx so the request layer can map it to
			// E_AUTH instead of handing HTML to a JSON parser. Suffix match so a
			// base-path deployment (/archery/login/) is covered too.
			if isLoginPath(req.URL.Path) {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) > 0 {
				prev := via[len(via)-1]
				sameHost := strings.EqualFold(req.URL.Host, prev.URL.Host)
				if sameHost {
					if auth := prev.Header.Get("Authorization"); auth != "" {
						req.Header.Set("Authorization", auth)
					}
				}
				if ua := prev.Header.Get("User-Agent"); ua != "" {
					req.Header.Set("User-Agent", ua)
				}
			}
			return nil
		},
	}
}

// applyJSONHeaders sets REST-mode headers: Bearer token, JSON content type.
func (c *Client) applyJSONHeaders(req *http.Request, jsonBody bool) {
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent())
	if jsonBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

// applyInternalHeaders sets internal-mode headers: session cookie, form content type.
func (c *Client) applyInternalHeaders(req *http.Request) {
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	// Django's CSRF middleware rejects unsafe methods on a session without a
	// matching csrftoken; mirror the csrftoken cookie into the header.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		if csrf := c.csrfTokenFromJar(); csrf != "" {
			req.Header.Set("X-CSRFToken", csrf)
			req.Header.Set("Referer", c.host+"/")
		}
	}
}

// Code2FARequired is the sentinel APIError.Code value set when a login is
// blocked on a missing 2FA code. The cmd layer maps it to output.E_2FA_REQUIRED
// + the human-required exit code; carrying it as a plain string keeps the api
// package free of an output dependency.
const Code2FARequired = "E_2FA_REQUIRED"

// CodeValidation is the APIError.Code for a client-side validation failure (e.g.
// a wrong/expired 2FA code). It mirrors output.E_VALIDATION as a plain string.
const CodeValidation = "E_VALIDATION"

// APIError represents an error returned by the Archery API.
type APIError struct {
	StatusCode    int
	ErrorMessages []string
	Errors        map[string]string
	// Code, when non-empty, is an explicit error-code override the cmd layer uses
	// instead of deriving the code from StatusCode (e.g. Code2FARequired).
	Code string
}

func (e *APIError) Error() string {
	if len(e.ErrorMessages) > 0 {
		return fmt.Sprintf("Archery API error %d: %s", e.StatusCode, e.ErrorMessages[0])
	}
	if len(e.Errors) > 0 {
		return fmt.Sprintf("Archery API error %d: %v", e.StatusCode, e.Errors)
	}
	return fmt.Sprintf("Archery API error %d", e.StatusCode)
}

func maxRetries() int {
	if s := strings.TrimSpace(os.Getenv("ARCHERY_CLI_MAX_RETRIES")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return n
		}
	}
	return defaultMaxRetries
}

// rateLimitWait returns how long to wait after a 429 using Retry-After (capped).
func rateLimitWait(h http.Header) time.Duration {
	var wait time.Duration
	if ra := h.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			wait = time.Duration(secs) * time.Second
		}
	}
	if wait <= 0 {
		wait = time.Second
	}
	if wait > maxRateLimitWait {
		return maxRateLimitWait
	}
	return wait
}

func isRetryableNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "tls handshake timeout")
}

func serverErrorWait(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * time.Second
}

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func waitForRetry(ctx context.Context, attempt int, statusCode int, header http.Header) error {
	var wait time.Duration
	switch {
	case statusCode == http.StatusTooManyRequests:
		wait = rateLimitWait(header)
	case statusCode >= 500:
		wait = serverErrorWait(attempt)
	default:
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Transport modes mirror config.ModeSession / config.ModeJWT. Session is the
// default: works for ordinary accounts via Archery's AJAX endpoints + Django
// session cookie. JWT is the opt-in REST transport.
const (
	ModeSession = "session"
	ModeJWT     = "jwt"
)

// Client wraps the Archery API HTTP client.
type Client struct {
	host         string
	mode         string
	accessToken  string
	refreshToken string
	httpClient   *http.Client
	Auth         *AuthAPI
	Workflows    *WorkflowAPI
	Binlog       *BinlogAPI
	Users        *UserAPI

	// Session-mode credentials for Archery's Django web endpoints (the default
	// transport): JWT alone cannot authenticate them — they need a session
	// cookie from a form login. ensureSession performs that login lazily on the
	// first session request, unless cached cookies were injected first.
	sessionUser  string
	sessionPass  string
	sessionReady bool

	// otp is an optional 6-digit TOTP code supplied for accounts with 2FA
	// enabled. It is consumed once during the /authenticate/ + /api/v1/user/2fa/
	// handshake; the resulting sessionid is cached so later commands need no OTP
	// (until the session expires). 2FA codes are ~30s-lived, so it must be fresh.
	otp string

	// onSessionEstablished, when set, is invoked with the freshly authenticated
	// Django cookies right after a lazy form login succeeds, so the command layer
	// can persist them (keyring) and reuse the session across later commands —
	// avoiding a fresh /authenticate/ (+2FA OTP) on every session-mode call.
	onSessionEstablished func(sessionID, csrfToken string)

	// sessionInjected is true when sessionReady was set by restoring a cached
	// cookie (InjectSessionCookies) rather than a fresh form login. It lets the
	// request layer recover once (clear + re-login) when the server rejects a
	// stale cached session, instead of dead-ending at E_AUTH.
	sessionInjected bool
}

// NewClient creates a new API client using global HTTP options. The transport
// defaults to session mode; callers switch to JWT via SetMode(ModeJWT).
func NewClient(host string) *Client {
	c := &Client{
		host:       strings.TrimRight(host, "/"),
		mode:       ModeSession,
		httpClient: newHTTPClient(globalHTTP),
	}
	c.Auth = &AuthAPI{client: c}
	c.Workflows = &WorkflowAPI{client: c}
	c.Binlog = &BinlogAPI{client: c}
	c.Users = &UserAPI{client: c}
	return c
}

// SetMode selects the transport: ModeSession (default) or ModeJWT. Any other
// value falls back to session.
func (c *Client) SetMode(mode string) {
	if strings.EqualFold(strings.TrimSpace(mode), ModeJWT) {
		c.mode = ModeJWT
		return
	}
	c.mode = ModeSession
}

// Mode returns the current transport mode.
func (c *Client) Mode() string {
	if c.mode == "" {
		return ModeSession
	}
	return c.mode
}

// SetTokens sets the access and refresh tokens for REST-mode requests.
func (c *Client) SetTokens(accessToken, refreshToken string) {
	c.accessToken = accessToken
	c.refreshToken = refreshToken
}

// SetSessionCredentials supplies the username/password used to establish a
// Django session for internal-mode (legacy web endpoint) commands. JWT-only
// configs (keyring) leave these empty; ensureSession then returns a clear
// error directing the caller to provide credentials.
func (c *Client) SetSessionCredentials(username, password string) {
	c.sessionUser = username
	c.sessionPass = password
}

// SetOTP supplies a 6-digit 2FA code used to complete login for accounts with
// two-factor authentication enabled. Empty means "no code provided": ensureSession
// then surfaces an E_2FA_REQUIRED error when the account demands one.
func (c *Client) SetOTP(otp string) {
	c.otp = strings.TrimSpace(otp)
}

// SetOnSessionEstablished registers a callback invoked with the freshly
// authenticated Django cookies right after a lazy form login completes, letting
// the command layer persist and reuse the session. No-op if never set.
func (c *Client) SetOnSessionEstablished(fn func(sessionID, csrfToken string)) {
	c.onSessionEstablished = fn
}

// markSessionReady marks the session authenticated and notifies any persistence
// hook with the current cookies. Used only on the fresh-login success paths
// (not InjectSessionCookies, which restores an already-persisted session).
func (c *Client) markSessionReady() {
	c.sessionReady = true
	c.sessionInjected = false // a fresh login supersedes any injected session
	if c.onSessionEstablished != nil {
		if sid, csrf := c.ExportSessionCookies(); sid != "" {
			c.onSessionEstablished(sid, csrf)
		}
	}
}

// recoverInjectedSession attempts a one-shot transparent re-login when a request
// is bounced to /login/ because a *cached* (injected) session was rejected by the
// server. Returns (true, nil) when a fresh session is established (retry the
// request); (false, err) when re-login fails with an actionable error (e.g.
// E_2FA_REQUIRED); or (false, nil) when recovery does not apply (session was
// freshly minted this run, or no credentials are available) — the caller then
// surfaces the generic auth-redirect error.
func (c *Client) recoverInjectedSession(ctx context.Context) (bool, error) {
	if !c.sessionInjected {
		return false, nil
	}
	if strings.TrimSpace(c.sessionUser) == "" || strings.TrimSpace(c.sessionPass) == "" {
		return false, nil
	}
	c.clearSessionCookies()
	c.sessionReady = false
	c.sessionInjected = false
	if err := c.ensureSession(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// currentSessionID returns the sessionid cookie value from the jar, or "".
func (c *Client) currentSessionID() string {
	u, err := url.Parse(c.host)
	if err != nil {
		return ""
	}
	for _, ck := range c.httpClient.Jar.Cookies(u) {
		if ck.Name == "sessionid" {
			return ck.Value
		}
	}
	return ""
}

// InjectSessionCookies seeds the cookie jar with cached Django cookies so that
// session requests can skip the interactive form login. Empty values are
// ignored. When a sessionid is present the client is marked ready, avoiding a
// redundant /authenticate/ round-trip on the first call.
func (c *Client) InjectSessionCookies(sessionID, csrfToken string) {
	u, err := url.Parse(c.host)
	if err != nil {
		return
	}
	var cookies []*http.Cookie
	if sessionID != "" {
		cookies = append(cookies, &http.Cookie{Name: "sessionid", Value: sessionID, Path: "/"})
	}
	if csrfToken != "" {
		cookies = append(cookies, &http.Cookie{Name: "csrftoken", Value: csrfToken, Path: "/"})
	}
	if len(cookies) == 0 {
		return
	}
	c.httpClient.Jar.SetCookies(u, cookies)
	if sessionID != "" {
		c.sessionReady = true
		c.sessionInjected = true
	}
}

// ExportSessionCookies returns the current sessionid and csrftoken from the jar,
// for persistence after a successful login. Either value may be empty.
func (c *Client) ExportSessionCookies() (sessionID, csrfToken string) {
	u, err := url.Parse(c.host)
	if err != nil {
		return "", ""
	}
	for _, ck := range c.httpClient.Jar.Cookies(u) {
		switch ck.Name {
		case "sessionid":
			sessionID = ck.Value
		case "csrftoken":
			csrfToken = ck.Value
		}
	}
	return sessionID, csrfToken
}

// csrfTokenFromJar returns the Django csrftoken cookie value, if present.
func (c *Client) csrfTokenFromJar() string {
	u, err := url.Parse(c.host)
	if err != nil {
		return ""
	}
	for _, ck := range c.httpClient.Jar.Cookies(u) {
		if ck.Name == "csrftoken" {
			return ck.Value
		}
	}
	return ""
}

// ensureSession lazily establishes a Django session cookie for internal-mode
// requests. It performs Archery's form login: GET /login/ to obtain the
// csrftoken cookie, then POST /login/ with csrfmiddlewaretoken + credentials.
// The cookie jar carries csrftoken/sessionid across the two calls and into the
// subsequent internal request. Idempotent: a no-op once the session is ready.
func (c *Client) ensureSession(ctx context.Context) error {
	if c.sessionReady {
		return nil
	}
	if strings.TrimSpace(c.sessionUser) == "" || strings.TrimSpace(c.sessionPass) == "" {
		return &APIError{
			StatusCode:    http.StatusUnauthorized,
			ErrorMessages: []string{"this command uses Archery's session-based endpoints, which need a username and password; set ARCHERY_CLI_USERNAME and ARCHERY_CLI_PASSWORD (a cached JWT alone is not enough)"},
		}
	}

	// A fresh form-login must start from an empty jar. If an already-authenticated
	// sessionid is present (e.g. a stale cached cookie that lazy-login fell through
	// on), Archery's /authenticate/ takes a branch that returns a session_key in
	// `data` and issues no new sessionid Set-Cookie, so hasSessionCookie() stays
	// false and login spuriously "fails". Clearing first makes login deterministic.
	c.clearSessionCookies()

	// Step 1: prime the csrftoken cookie.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/login/", nil)
	if err != nil {
		return fmt.Errorf("session login (csrf prime): %w", err)
	}
	getReq.Header.Set("User-Agent", defaultUserAgent())
	getResp, err := c.httpClient.Do(getReq)
	if err != nil {
		return fmt.Errorf("session login (csrf prime): %w", err)
	}
	_, _ = io.Copy(io.Discard, getResp.Body)
	_ = getResp.Body.Close()

	csrf := c.csrfTokenFromJar()

	// Step 2: post credentials to Archery's AJAX auth endpoint. On success it
	// returns JSON {"status":0,...} and calls Django login() to set the
	// sessionid cookie; on failure {"status":1,"msg":"..."}. Django validates
	// the csrfmiddlewaretoken form field against the csrftoken cookie and
	// checks Referer for same-origin.
	form := url.Values{
		"username":            {c.sessionUser},
		"password":            {c.sessionPass},
		"csrfmiddlewaretoken": {csrf},
	}
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/authenticate/", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("session login: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", defaultUserAgent())
	postReq.Header.Set("Referer", c.host+"/login/")
	if csrf != "" {
		postReq.Header.Set("X-CSRFToken", csrf)
	}
	postResp, err := c.httpClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("session login: %w", err)
	}
	body, _ := io.ReadAll(postResp.Body)
	_ = postResp.Body.Close()

	// Fail closed: a non-2xx response (incl. a redirect to /login/ stopped by
	// CheckRedirect) means the login failed; never read it as a zero-value
	// success.
	if postResp.StatusCode >= 400 {
		return c.parseError(postResp.StatusCode, body)
	}
	if postResp.StatusCode >= 300 {
		return c.authRedirectError(postResp.Header.Get("Location"))
	}

	// Archery's /authenticate/ has two success branches (verified on v1.8.5):
	//   no 2FA  -> {"status":0,"msg":"ok","data":null}      + sessionid cookie
	//   2FA on  -> {"status":0,"msg":"ok","data":"<key>"}   + only a temp cookie,
	//              NOT logged in yet; the session_key in `data` must be completed
	//              via POST /api/v1/user/2fa/verify/ with the OTP.
	// A status!=0 carries Archery's reason (wrong password, etc.).
	var ar struct {
		Status int             `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		// A 2xx that isn't the expected JSON (e.g. an HTML page) must fail closed.
		return &APIError{
			StatusCode:    http.StatusUnauthorized,
			ErrorMessages: []string{"session login returned an unexpected (non-JSON) response: " + bodySnippet(body)},
		}
	}
	if ar.Status == 0 && c.hasSessionCookie() {
		c.markSessionReady()
		return nil
	}

	// status==0 but no sessionid and a non-empty string `data` means the password
	// was accepted and the account needs a second factor to finish logging in.
	if ar.Status == 0 && !c.hasSessionCookie() && sessionKeyFromData(ar.Data) != "" {
		return c.complete2FA(ctx, csrf, sessionKeyFromData(ar.Data))
	}

	msg := strings.TrimSpace(ar.Msg)
	if msg == "" {
		msg = "check ARCHERY_CLI_USERNAME / ARCHERY_CLI_PASSWORD"
	}
	return &APIError{
		StatusCode:    http.StatusUnauthorized,
		ErrorMessages: []string{"session login failed: " + msg},
	}
}

// sessionKeyFromData returns the session_key string Archery puts in the
// /authenticate/ `data` field when an account has 2FA enabled. `data` is null
// (or absent) for non-2FA accounts, so an empty return means "no 2FA pending".
func sessionKeyFromData(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// complete2FA finishes a login that /authenticate/ left pending on a second
// factor. With no OTP supplied it returns E_2FA_REQUIRED so the agent knows to
// retry with --otp. With an OTP it POSTs to /api/v1/user/2fa/verify/ (DRF,
// AllowAny) — NOT /api/v1/user/2fa/, which is the 2FA *config* endpoint and
// rejects a verify payload — using the temporary cookie + CSRF already in the
// jar; on success Archery's TwoFAVerify view calls login() and sets the
// sessionid, which we then mark ready. For an already-configured account the
// server reads auth_type from its own config, so engineer + otp suffice.
func (c *Client) complete2FA(ctx context.Context, csrf, sessionKey string) error {
	if c.otp == "" {
		return &APIError{
			StatusCode:    http.StatusUnauthorized,
			Code:          Code2FARequired,
			ErrorMessages: []string{"该 Archery 账号开启了 2FA，需要 6 位验证码，请加 --otp <code> 重试"},
		}
	}

	// /authenticate/ stashed the password-verified user in a fresh temp session
	// and returned ONLY its session_key in the response body (no Set-Cookie). The
	// /2fa/verify/ view resolves that pending user via request.session, so we must
	// replay the key as the sessionid cookie; without it the view rejects the OTP
	// with "需先校验用户密码！" even when the code is correct. On success Archery's
	// login() issues a new authenticated sessionid that overwrites this one.
	if sessionKey != "" {
		if u, err := url.Parse(c.host); err == nil {
			c.httpClient.Jar.SetCookies(u, []*http.Cookie{{Name: "sessionid", Value: sessionKey, Path: "/"}})
		}
	}

	form := url.Values{
		"engineer":  {c.sessionUser},
		"otp":       {c.otp},
		"auth_type": {twoFAAuthType()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/v1/user/2fa/verify/", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("2fa verify: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultUserAgent())
	req.Header.Set("Referer", c.host+"/login/")
	if csrf != "" {
		req.Header.Set("X-CSRFToken", csrf)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("2fa verify: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	// Fail closed — a 2FA verify is successful ONLY when the server positively
	// confirms it. ① a non-2xx (e.g. 400 "auth_type required") is a failure;
	// ② the Archery envelope must carry status==0; ③ Django's login() rotates
	// the session key, so the sessionid MUST change away from the temp
	// session_key we replayed — otherwise we are NOT logged in. hasSessionCookie()
	// alone is meaningless because we injected that temp key ourselves.
	if resp.StatusCode >= 400 {
		return twoFAError(c.parseError(resp.StatusCode, body))
	}
	var ar struct {
		Status *int   `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		return &APIError{
			StatusCode:    http.StatusBadRequest,
			Code:          CodeValidation,
			ErrorMessages: []string{"2FA 验证返回了非预期（非 JSON）响应：" + bodySnippet(body)},
		}
	}
	if ar.Status != nil && *ar.Status == 0 {
		if sid := c.currentSessionID(); sid != "" && sid != sessionKey {
			c.markSessionReady()
			return nil
		}
	}
	msg := strings.TrimSpace(ar.Msg)
	if msg == "" {
		msg = "2FA 验证码错误或已过期（2FA 验证码有效期约 30 秒，请重新生成后重试）；或账号 2FA 类型与 auth_type 不符"
	}
	return &APIError{
		StatusCode:    http.StatusBadRequest,
		Code:          CodeValidation,
		ErrorMessages: []string{"2FA 验证码错误：" + msg},
	}
}

// twoFAAuthType returns the 2FA mechanism sent to Archery's verify endpoint.
// Archery requires this field; it defaults to "totp" (authenticator apps, which
// a 6-digit --otp implies) and can be overridden for SMS-based 2FA.
func twoFAAuthType() string {
	if v := strings.TrimSpace(os.Getenv("ARCHERY_CLI_2FA_TYPE")); v != "" {
		return v
	}
	return "totp"
}

// twoFAError normalizes a verify failure into an E_VALIDATION-class APIError so
// the command layer reports a wrong/expired code (or missing field) as a
// validation problem, not a retryable server error.
func twoFAError(e *APIError) *APIError {
	if e == nil {
		e = &APIError{StatusCode: http.StatusBadRequest}
	}
	e.Code = CodeValidation
	if len(e.ErrorMessages) == 0 && len(e.Errors) == 0 {
		e.ErrorMessages = []string{"2FA 验证失败"}
	}
	return e
}

// clearSessionCookies evicts any sessionid/csrftoken from the jar by overwriting
// them with already-expired cookies. net/http/cookiejar has no delete API, so an
// expired cookie is the supported way to drop one. Used before a fresh form-login
// so a stale cached cookie can't poison Archery's /authenticate/ branch selection.
func (c *Client) clearSessionCookies() {
	u, err := url.Parse(c.host)
	if err != nil {
		return
	}
	expired := time.Unix(0, 0)
	c.httpClient.Jar.SetCookies(u, []*http.Cookie{
		{Name: "sessionid", Value: "", Path: "/", Expires: expired, MaxAge: -1},
		{Name: "csrftoken", Value: "", Path: "/", Expires: expired, MaxAge: -1},
	})
}

// hasSessionCookie reports whether the jar holds a non-empty sessionid cookie.
func (c *Client) hasSessionCookie() bool {
	u, err := url.Parse(c.host)
	if err != nil {
		return false
	}
	for _, ck := range c.httpClient.Jar.Cookies(u) {
		if ck.Name == "sessionid" && ck.Value != "" {
			return true
		}
	}
	return false
}

// Host returns the underlying Archery host (without trailing slash).
func (c *Client) Host() string { return c.host }

// RefreshToken returns the current refresh token.
func (c *Client) RefreshToken() string { return c.refreshToken }

// CookieJar returns the underlying cookie jar for session-based auth.
func (c *Client) CookieJar() http.CookieJar { return c.httpClient.Jar }

// APIPath returns "/api" + the relative resource path.
func (c *Client) APIPath(resource string) string {
	if !strings.HasPrefix(resource, "/") {
		resource = "/" + resource
	}
	return APIBasePath + resource
}

// ─── REST request mode (JSON, Bearer token) ─────────────────────────────────

// restRequest executes a JSON request with Bearer token auth and retry logic.
//
//	429 -> respect Retry-After
//	5xx -> exponential backoff (1s, 2s, 4s)
//	4xx -> no retry
func (c *Client) restRequest(ctx context.Context, method, path string, body any) ([]byte, int, http.Header, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}

		var reqBody io.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("encoding request body: %w", err)
			}
			reqBody = bytes.NewReader(encoded)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.host+path, reqBody)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("creating request: %w", err)
		}

		c.applyJSONHeaders(req, body != nil)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if isRetryableNetworkErr(err) && attempt < maxRetries() {
				continue
			}
			return nil, 0, nil, fmt.Errorf("executing request: %w", err)
		}

		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, resp.Header, fmt.Errorf("reading response body: %w", readErr)
		}

		statusCode := resp.StatusCode
		header := resp.Header

		// A 3xx that survived CheckRedirect is an auth redirect to /login/; treat
		// it as E_AUTH rather than letting an empty/HTML body reach a JSON parser.
		if statusCode >= 300 && statusCode < 400 {
			return nil, statusCode, header, c.authRedirectError(header.Get("Location"))
		}
		if statusCode < 400 {
			return data, statusCode, header, nil
		}

		if shouldRetry(statusCode) {
			if attempt >= maxRetries() {
				return nil, statusCode, header, c.parseError(statusCode, data)
			}
			if err := waitForRetry(ctx, attempt, statusCode, header); err != nil {
				return nil, 0, nil, err
			}
			continue
		}

		return nil, statusCode, header, c.parseError(statusCode, data)
	}
}

// ─── Internal request mode (form-encoded, session cookie) ───────────────────

// internalRequest executes a form-encoded request with session cookie auth
// and retry logic. Used for Archery's internal Django endpoints that use
// session-based authentication instead of JWT.
func (c *Client) internalRequest(ctx context.Context, method, path string, form url.Values) ([]byte, int, http.Header, error) {
	// Internal Django endpoints need a session cookie, not just a JWT.
	if err := c.ensureSession(ctx); err != nil {
		return nil, 0, nil, err
	}
	triedRecovery := false
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}

		var reqBody io.Reader
		if form != nil {
			reqBody = strings.NewReader(form.Encode())
		}

		req, err := http.NewRequestWithContext(ctx, method, c.host+path, reqBody)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("creating request: %w", err)
		}

		c.applyInternalHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if isRetryableNetworkErr(err) && attempt < maxRetries() {
				continue
			}
			return nil, 0, nil, fmt.Errorf("executing request: %w", err)
		}

		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, resp.Header, fmt.Errorf("reading response body: %w", readErr)
		}

		statusCode := resp.StatusCode
		header := resp.Header

		// A 3xx that survived CheckRedirect is an auth redirect to /login/. If the
		// session was restored from cache (injected) and the server has since
		// rejected it, transparently clear it and re-login ONCE before giving up —
		// otherwise a stale cached cookie would dead-end every session command at
		// E_AUTH (CLI-SPEC §16.1: refreshable creds should refresh transparently).
		// A freshly-logged-in session that still bounces is a real auth failure.
		if statusCode >= 300 && statusCode < 400 {
			if !triedRecovery {
				triedRecovery = true
				if recovered, rerr := c.recoverInjectedSession(ctx); recovered {
					continue
				} else if rerr != nil {
					return nil, statusCode, header, rerr
				}
			}
			return nil, statusCode, header, c.authRedirectError(header.Get("Location"))
		}
		if statusCode < 400 {
			return data, statusCode, header, nil
		}

		if shouldRetry(statusCode) {
			if attempt >= maxRetries() {
				return nil, statusCode, header, c.parseError(statusCode, data)
			}
			if err := waitForRetry(ctx, attempt, statusCode, header); err != nil {
				return nil, 0, nil, err
			}
			continue
		}

		return nil, statusCode, header, c.parseError(statusCode, data)
	}
}

// isLoginPath reports whether p is Archery's login page, tolerating a trailing
// slash and a deployment base path (e.g. /archery/login/ or /login/).
func isLoginPath(p string) bool {
	return strings.HasSuffix(strings.TrimRight(p, "/"), "/login")
}

// authRedirectError converts a 3xx redirect (typically to /login/) into an
// E_AUTH-class APIError. Archery's web endpoints redirect unauthenticated
// requests to the HTML login page; following it would hand HTML to a JSON
// parser. StatusCode 401 maps to E_AUTH (exit 4) at the command layer.
func (c *Client) authRedirectError(location string) *APIError {
	msg := "Archery redirected to the login page: not authenticated (session expired or invalid)."
	if loc := strings.TrimSpace(location); loc != "" {
		msg = "Archery redirected to " + loc + ": not authenticated (session expired or invalid)."
	}
	msg += " Re-login (for jwt regions with 2FA, re-run with --otp <code>)."
	return &APIError{StatusCode: http.StatusUnauthorized, ErrorMessages: []string{msg}}
}

// bodySnippet returns a trimmed, length-capped preview of a response body for
// diagnostics when a response cannot be parsed as expected.
func bodySnippet(b []byte) string {
	const max = 256
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// parseError converts an HTTP status code and response body into an APIError.
//
// Archery returns two error shapes:
//
// DRF standard:
//
//	{"detail": "Not found."}
//	{"field_name": ["error message"]}
//
// Archery internal:
//
//	{"status": 1, "msg": "error message", "data": null}
func (c *Client) parseError(statusCode int, data []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}

	if len(data) > 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err == nil {
			// DRF "detail" string
			if detail, ok := raw["detail"]; ok {
				var s string
				if err := json.Unmarshal(detail, &s); err == nil && s != "" {
					apiErr.ErrorMessages = []string{s}
				}
			}
			// DRF per-field validation errors
			for k, v := range raw {
				if k == "detail" || k == "status" || k == "msg" || k == "data" {
					continue
				}
				var msgs []string
				if err := json.Unmarshal(v, &msgs); err == nil && len(msgs) > 0 {
					if apiErr.Errors == nil {
						apiErr.Errors = make(map[string]string)
					}
					apiErr.Errors[k] = strings.Join(msgs, "; ")
				}
			}
			// Archery internal format: {"status": 1, "msg": "...", "data": ...}
			if msg, ok := raw["msg"]; ok {
				var s string
				if err := json.Unmarshal(msg, &s); err == nil && s != "" {
					apiErr.ErrorMessages = append(apiErr.ErrorMessages, s)
				}
			}
		}
	}

	// Friendly fallbacks for known status codes.
	switch statusCode {
	case http.StatusUnauthorized:
		if len(apiErr.ErrorMessages) == 0 {
			apiErr.ErrorMessages = []string{"not logged in: run 'archery-cli auth login'"}
		}
	case http.StatusForbidden:
		if len(apiErr.ErrorMessages) == 0 {
			apiErr.ErrorMessages = []string{"permission denied: check your credentials and group membership"}
		}
	case http.StatusNotFound:
		if len(apiErr.ErrorMessages) == 0 {
			apiErr.ErrorMessages = []string{"resource not found"}
		}
	}

	if len(apiErr.ErrorMessages) == 0 && len(apiErr.Errors) == 0 {
		apiErr.ErrorMessages = []string{fmt.Sprintf("unexpected status code %d", statusCode)}
	}
	return apiErr
}

// Pagination holds page metadata from DRF response headers or response body.
//
// Archery uses Django REST Framework. Depending on the paginator configured
// server-side, pagination info may arrive as:
//   - HTTP headers: X-Total-Count, Link (RFC 5988)
//   - JSON body: {"count": N, "next": "url", "previous": "url", "results": [...]}
//
// This struct captures both sources. The JSON-body fields (Count, Next,
// Previous) come from PaginatedResponse; the header fields are populated
// by extractPagination when the caller uses GetWithPagination.
type Pagination struct {
	// Count is the total number of items (from X-Total-Count header or JSON "count").
	Count int
	// Page is the current page number (from X-Page header, 1-based).
	Page int
	// PerPage is the page size (from X-Per-Page header).
	PerPage int
	// NextPage is the next page number (0 if last page).
	NextPage int
	// PrevPage is the previous page number (0 if first page).
	PrevPage int
	// Link is the raw Link header value (RFC 5988).
	Link string
}

// extractPagination parses DRF-style pagination from HTTP response headers.
//
// Supported headers:
//   - X-Total-Count: total item count
//   - X-Page, X-Per-Page, X-Next-Page, X-Prev-Page: page navigation
//   - Link: RFC 5988 Link header (parsed for rel="next" / rel="prev")
//
// If the Link header is present, NextPage/PrevPage are derived from it
// (overriding the X-*-Page headers).
func extractPagination(h http.Header) Pagination {
	atoi := func(s string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		return n
	}
	p := Pagination{
		Count:    atoi(h.Get("X-Total-Count")),
		Page:     atoi(h.Get("X-Page")),
		PerPage:  atoi(h.Get("X-Per-Page")),
		NextPage: atoi(h.Get("X-Next-Page")),
		PrevPage: atoi(h.Get("X-Prev-Page")),
		Link:     h.Get("Link"),
	}
	// Parse Link header for next/prev page numbers if present.
	if p.Link != "" {
		parseLinkHeader(&p)
	}
	return p
}

// parseLinkHeader extracts page numbers from an RFC 5988 Link header.
// Example: <https://host/api/v1/workflow/?limit=20&offset=20>; rel="next"
func parseLinkHeader(p *Pagination) {
	for _, part := range strings.Split(p.Link, ",") {
		part = strings.TrimSpace(part)
		// Split into URL and params: <url>; rel="next"
		segments := strings.SplitN(part, ";", 2)
		if len(segments) != 2 {
			continue
		}
		rel := ""
		for _, param := range segments[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "rel=") {
				rel = strings.Trim(strings.TrimPrefix(param, "rel="), `"`)
			}
		}
		// Extract page/offset from the URL to determine page number.
		urlPart := strings.TrimSpace(segments[0])
		urlPart = strings.Trim(urlPart, "<>")
		if parsed, err := url.Parse(urlPart); err == nil {
			q := parsed.Query()
			page := 0
			if v := q.Get("page"); v != "" {
				page, _ = strconv.Atoi(v)
			} else if v := q.Get("offset"); v != "" {
				offset, _ := strconv.Atoi(v)
				limit := p.PerPage
				if limit > 0 {
					page = offset/limit + 1
				}
			}
			if page > 0 {
				switch rel {
				case "next":
					p.NextPage = page
				case "prev":
					p.PrevPage = page
				}
			}
		}
	}
}

// TagUntrusted adds a "_untrusted" key to data listing the names of fields that
// contain externally-sourced, user-generated content. Callers must pass only the
// inner data map (not the full envelope). Fields that do not exist in the map
// are silently skipped.
func TagUntrusted(data map[string]any, fields ...string) {
	var untrusted []string
	for _, f := range fields {
		if _, ok := data[f]; ok {
			untrusted = append(untrusted, f)
		}
	}
	if len(untrusted) > 0 {
		data["_untrusted"] = untrusted
	}
}

// ─── Convenience verbs (REST mode) ──────────────────────────────────────────

// Get sends a GET request with Bearer token auth. Returns body bytes only.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	data, _, _, err := c.restRequest(ctx, http.MethodGet, path, nil)
	return data, err
}

// GetWithPagination sends a GET request and returns body + pagination metadata
// from the response headers. Useful for list endpoints.
func (c *Client) GetWithPagination(ctx context.Context, path string) ([]byte, Pagination, error) {
	data, _, header, err := c.restRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, Pagination{}, err
	}
	return data, extractPagination(header), nil
}

// Post sends a POST request with Bearer token auth.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	data, _, _, err := c.restRequest(ctx, http.MethodPost, path, body)
	return data, err
}

// Put sends a PUT request with Bearer token auth.
func (c *Client) Put(ctx context.Context, path string, body any) ([]byte, error) {
	data, _, _, err := c.restRequest(ctx, http.MethodPut, path, body)
	return data, err
}

// Delete sends a DELETE request with Bearer token auth.
func (c *Client) Delete(ctx context.Context, path string) error {
	_, _, _, err := c.restRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// ─── Convenience verbs (Internal mode) ──────────────────────────────────────

// SessionGet sends a GET request with Django session cookie auth.
// Canonical name for the default (session) transport; InternalGet is kept as an
// alias used throughout the command layer.
func (c *Client) SessionGet(ctx context.Context, path string) ([]byte, error) {
	return c.InternalGet(ctx, path)
}

// SessionPost sends a form-encoded POST with Django session cookie auth.
func (c *Client) SessionPost(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return c.InternalPost(ctx, path, form)
}

// InternalGet sends a GET request with session cookie auth.
func (c *Client) InternalGet(ctx context.Context, path string) ([]byte, error) {
	data, _, _, err := c.internalRequest(ctx, http.MethodGet, path, nil)
	return data, err
}

// InternalPost sends a POST request with session cookie auth.
func (c *Client) InternalPost(ctx context.Context, path string, form url.Values) ([]byte, error) {
	data, _, _, err := c.internalRequest(ctx, http.MethodPost, path, form)
	return data, err
}

// SessionPostExpectRedirect posts a form to a Django view that signals success
// with a 302 redirect to the new object's detail page, and returns the object
// id parsed from the Location header (e.g. /detail/42/ -> 42). Archery's submit
// views render error.html with HTTP 200 on a validation failure instead of
// redirecting, so a 200 (non-redirect) is reported as an error carrying the
// page's message hint. This does not follow the redirect: the id is in the
// Location header, and following it would lose it.
func (c *Client) SessionPostExpectRedirect(ctx context.Context, path string, form url.Values) (int, error) {
	if err := c.ensureSession(ctx); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}
	c.applyInternalHeaders(req)
	// RoundTrip bypasses the http.Client's cookie jar, so attach the session
	// cookies (sessionid + csrftoken) by hand.
	if u, perr := url.Parse(c.host); perr == nil {
		for _, ck := range c.httpClient.Jar.Cookies(u) {
			req.AddCookie(ck)
		}
	}

	// Issue a single request without following the redirect so the Location
	// header survives.
	resp, err := c.httpClient.Transport.RoundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("executing request: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if id := lastPathID(loc); id > 0 {
			return id, nil
		}
		return 0, &APIError{
			StatusCode:    resp.StatusCode,
			ErrorMessages: []string{"workflow submitted but the server did not return a workflow id in the redirect"},
		}
	}

	if resp.StatusCode >= 400 {
		return 0, c.parseError(resp.StatusCode, body)
	}

	// HTTP 200 without a redirect means the view rendered error.html: the submit
	// was rejected. Surface a generic reason; the exact message is HTML.
	return 0, &APIError{
		StatusCode:    http.StatusBadRequest,
		ErrorMessages: []string{"workflow submission was rejected by Archery (check instance/group permissions, db name, and SQL)"},
	}
}

// lastPathID extracts the trailing integer path segment from a URL or path,
// e.g. "/detail/42/" or "https://h/sqlworkflow/42/" -> 42. Returns 0 if none.
func lastPathID(loc string) int {
	if loc == "" {
		return 0
	}
	if u, err := url.Parse(loc); err == nil {
		loc = u.Path
	}
	for _, seg := range strings.Split(strings.Trim(loc, "/"), "/") {
		if n, err := strconv.Atoi(seg); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// InternalPut sends a PUT request with session cookie auth.
func (c *Client) InternalPut(ctx context.Context, path string, form url.Values) ([]byte, error) {
	data, _, _, err := c.internalRequest(ctx, http.MethodPut, path, form)
	return data, err
}

// InternalDelete sends a DELETE request with session cookie auth.
func (c *Client) InternalDelete(ctx context.Context, path string) error {
	_, _, _, err := c.internalRequest(ctx, http.MethodDelete, path, nil)
	return err
}
