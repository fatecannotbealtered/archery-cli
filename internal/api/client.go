package api

import (
	"bytes"
	"context"
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

// defaultUserAgent returns ARCHERY_CLI_USER_AGENT if set, otherwise a clear CLI UA.
func defaultUserAgent() string {
	if ua := strings.TrimSpace(os.Getenv("ARCHERY_CLI_USER_AGENT")); ua != "" {
		return ua
	}
	return "archery-cli"
}

func newHTTPClient(timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout: timeout,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
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
}

// APIError represents an error returned by the Archery API.
type APIError struct {
	StatusCode    int
	ErrorMessages []string
	Errors        map[string]string
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

// Client wraps the Archery API HTTP client.
type Client struct {
	host         string
	accessToken  string
	refreshToken string
	httpClient   *http.Client
	Auth         *AuthAPI
	Workflows    *WorkflowAPI
}

// NewClient creates a new API client.
func NewClient(host string) *Client {
	c := &Client{
		host:       strings.TrimRight(host, "/"),
		httpClient: newHTTPClient(30 * time.Second),
	}
	c.Auth = &AuthAPI{client: c}
	c.Workflows = &WorkflowAPI{client: c}
	return c
}

// SetTokens sets the access and refresh tokens for REST-mode requests.
func (c *Client) SetTokens(accessToken, refreshToken string) {
	c.accessToken = accessToken
	c.refreshToken = refreshToken
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
