package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
)

// Fail-closed contract for the in-process signature gate (CLI-SPEC §14): a
// missing bundle is refused (no skip), a failing verification aborts, and only
// a successful verification yields "verified". The error CODE matters too: a
// missing bundle and a failed verdict are E_INTEGRITY (non-retryable), but a
// network failure fetching the bundle stays a retryable network class.
func TestVerifyUpdateChecksumSignature_FailClosed(t *testing.T) {
	tmp := t.TempDir()

	status, code, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/checksums.txt", "", tmp)
	if err == nil {
		t.Fatal("missing signature bundle must be refused")
	} else if !strings.Contains(err.Error(), "unsigned release") {
		t.Fatalf("unexpected error for missing bundle: %v", err)
	}
	if status != "missing" || code != output.E_INTEGRITY {
		t.Fatalf("missing bundle must be E_INTEGRITY, got status=%q code=%q", status, code)
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
	status, _, err = verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", srv.URL+"/b.json", tmp)
	if err != nil || status != "verified" {
		t.Fatalf("expected verified, got status=%q err=%v", status, err)
	}

	updateVerifySignature = func(_, _, _ string) error { return errors.New("certificate identity mismatch") }
	status, code, err = verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", srv.URL+"/b.json", tmp)
	if err == nil {
		t.Fatal("signature verification failure must abort")
	}
	if status != "failed" || code != output.E_INTEGRITY {
		t.Fatalf("verify failure must be E_INTEGRITY, got status=%q code=%q", status, code)
	}
}

// A network failure DOWNLOADING the signature bundle is a retryable network
// class, NOT an E_INTEGRITY forged-release verdict an agent must stop and report
// (CLI-SPEC §14: only signature/checksum verdicts are E_INTEGRITY).
func TestVerifyUpdateChecksumSignature_BundleDownloadNetworkClass(t *testing.T) {
	tmp := t.TempDir()
	origDownload := updateDownloadHook
	defer func() { updateDownloadHook = origDownload }()

	// 503 from the bundle endpoint -> retryable E_SERVER, not E_INTEGRITY.
	updateDownloadHook = func(context.Context, string, string) error {
		return &updateHTTPError{StatusCode: http.StatusServiceUnavailable, err: errors.New("503")}
	}
	status, code, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", "https://example.com/b.json", tmp)
	if err == nil {
		t.Fatal("bundle download failure must surface an error")
	}
	if status != "download_failed" {
		t.Fatalf("status = %q, want download_failed", status)
	}
	if code != output.E_SERVER {
		t.Fatalf("bundle download 503 must classify E_SERVER, got %q", code)
	}
	if !output.RetryableErrorCode(code) {
		t.Fatal("a bundle download network failure must be retryable")
	}
	if code == output.E_INTEGRITY {
		t.Fatal("a network failure must never be reported as an integrity failure")
	}
}

// A failure obtaining the Sigstore trust root (a TUF network refresh step) is a
// retryable network class, NOT an E_INTEGRITY forged-release verdict. The trust
// ANCHOR ships embedded; refreshing its metadata is network (CLI-SPEC §14).
func TestVerifyUpdateChecksumSignature_TrustRootNetworkClass(t *testing.T) {
	tmp := t.TempDir()
	origDownload := updateDownloadHook
	origVerify := updateVerifySignature
	defer func() { updateDownloadHook = origDownload; updateVerifySignature = origVerify }()

	updateDownloadHook = func(context.Context, string, string) error { return nil }
	updateVerifySignature = func(_, _, _ string) error {
		return fmt.Errorf("%w: dial tcp: i/o timeout", errTrustRootUnavailable)
	}
	status, code, err := verifyUpdateChecksumSignature(context.Background(), tmp+"/c.txt", "https://example.com/b.json", tmp)
	if err == nil {
		t.Fatal("trust root failure must surface an error")
	}
	if status != "trust_root_unavailable" {
		t.Fatalf("status = %q, want trust_root_unavailable", status)
	}
	if code != output.E_NETWORK || !output.RetryableErrorCode(code) {
		t.Fatalf("trust root failure must be retryable network, got %q", code)
	}
	if code == output.E_INTEGRITY {
		t.Fatal("trust root fetch failure must never be reported as integrity failure")
	}
}
