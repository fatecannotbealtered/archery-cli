package api

import (
	"context"
	"encoding/json"
	"fmt"
)

// AuthAPI wraps authentication-related API calls.
type AuthAPI struct{ client *Client }

// Login authenticates with username and password, storing tokens on success.
//
// POST /api/auth/token/
// Request:  {"username": "...", "password": "..."}
// Response: {"access": "...", "refresh": "..."}
func (a *AuthAPI) Login(ctx context.Context, username, password string) (string, string, error) {
	payload := map[string]string{
		"username": username,
		"password": password,
	}

	data, err := a.client.Post(ctx, a.client.APIPath("/auth/token/"), payload)
	if err != nil {
		return "", "", fmt.Errorf("login: %w", err)
	}

	var resp struct {
		Access  string `json:"access"`
		Refresh string `json:"refresh"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("parsing login response: %w", err)
	}
	if resp.Access == "" {
		return "", "", fmt.Errorf("login: server returned empty access token")
	}

	a.client.SetTokens(resp.Access, resp.Refresh)
	return resp.Access, resp.Refresh, nil
}

// LoginWithSession authenticates via Archery's session-based AJAX login and
// returns the resulting Django cookies for caching.
//
// Flow (verified against v1.8.5):
//
//	GET  /login/          -> Set-Cookie csrftoken
//	POST /authenticate/   form username/password/csrfmiddlewaretoken
//	                      + headers X-CSRFToken, Referer -> {"status":0,"msg":"ok"} + sessionid
//
// On success the cookie jar holds sessionid + (rotated) csrftoken, which are
// returned for persistence.
func (a *AuthAPI) LoginWithSession(ctx context.Context, username, password string) (sessionID, csrfToken string, err error) {
	a.client.SetSessionCredentials(username, password)
	a.client.sessionReady = false
	if err := a.client.ensureSession(ctx); err != nil {
		return "", "", err
	}
	sessionID, csrfToken = a.client.ExportSessionCookies()
	return sessionID, csrfToken, nil
}

// Refresh obtains a new access token using the stored refresh token.
//
// POST /api/auth/token/refresh/
// Request:  {"refresh": "..."}
// Response: {"access": "..."}
func (a *AuthAPI) Refresh(ctx context.Context, refreshToken string) (string, error) {
	payload := map[string]string{
		"refresh": refreshToken,
	}

	data, err := a.client.Post(ctx, a.client.APIPath("/auth/token/refresh/"), payload)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}

	var resp struct {
		Access string `json:"access"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing refresh response: %w", err)
	}
	if resp.Access == "" {
		return "", fmt.Errorf("refresh token: server returned empty access token")
	}

	a.client.SetTokens(resp.Access, refreshToken)
	return resp.Access, nil
}

// Verify checks whether a token is still valid.
//
// POST /api/auth/token/verify/
// Request:  {"token": "..."}
// Returns nil if valid, error otherwise.
func (a *AuthAPI) Verify(ctx context.Context, token string) error {
	payload := map[string]string{
		"token": token,
	}

	_, err := a.client.Post(ctx, a.client.APIPath("/auth/token/verify/"), payload)
	if err != nil {
		return fmt.Errorf("verify token: %w", err)
	}
	return nil
}
