package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fail-closed contract for the in-process signature gate (CLI-SPEC §14): a
// missing bundle is refused (no skip), a failing verification aborts, and only
// a successful verification yields "verified".
func TestVerifyUpdateChecksumSignature_FailClosed(t *testing.T) {
	tmp := t.TempDir()

	if _, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/checksums.txt", "", tmp); err == nil {
		t.Fatal("missing signature bundle must be refused")
	} else if !strings.Contains(err.Error(), "unsigned release") {
		t.Fatalf("unexpected error for missing bundle: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"bundle":"stub"}`))
	}))
	defer srv.Close()
	origClient := updateHTTPClient
	origVerify := updateVerifySignature
	defer func() { updateHTTPClient = origClient; updateVerifySignature = origVerify }()
	updateHTTPClient = srv.Client()

	updateVerifySignature = func(_, _, _ string) error { return nil }
	status, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", srv.URL+"/b.json", tmp)
	if err != nil || status != "verified" {
		t.Fatalf("expected verified, got status=%q err=%v", status, err)
	}

	updateVerifySignature = func(_, _, _ string) error { return errors.New("certificate identity mismatch") }
	if _, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", srv.URL+"/b.json", tmp); err == nil {
		t.Fatal("signature verification failure must abort")
	}
}
