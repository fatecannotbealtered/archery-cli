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

// NewClient creates a new API client using global HTTP options.
func NewClient(host string) *Client {
	c := &Client{
		host:       strings.TrimRight(host, "/"),
		httpClient: newHTTPClient(globalHTTP),
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
