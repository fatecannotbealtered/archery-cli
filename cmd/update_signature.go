package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	tuffetcher "github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// errTrustRootUnavailable marks a failure to obtain the Sigstore trust root.
// The trust ANCHOR ships embedded (sigstore-go's `root.json`), but REFRESHING
// the TUF metadata is a network step, so its failure — a hung/slow/DNS-failing
// Sigstore TUF registry, a refresh timeout, or any transport error — is a
// retryable network condition, NOT a forged-release verdict. The caller maps it
// onto the retryable network taxonomy rather than the non-retryable E_INTEGRITY
// reserved for a genuine signature/identity/checksum mismatch (CLI-SPEC §14).
var errTrustRootUnavailable = errors.New("sigstore trust root unavailable")

// updateTUFRefreshTimeout bounds the TUF trust-root refresh so a hung registry
// cannot stall the verify_signature stage indefinitely. The refresh is also
// cancelled when the command context is cancelled (SIGINT).
const updateTUFRefreshTimeout = 30 * time.Second

// updateOIDCIssuer is the GitHub Actions OIDC issuer. The release workflow's
// Sigstore certificate must be issued for this exact issuer.
const updateOIDCIssuer = "https://token.actions.githubusercontent.com"

// updateSignerIdentityRegexp pins the certificate SAN to this repo's tagged
// release workflow. Forging a signature that matches would require minting a
// GitHub OIDC token for this exact workflow path on tag refs — i.e. compromising
// the repository's CI, not breaking the cryptography. Anchored ^...$ so it
// cannot be satisfied by a looser workflow whose identity merely contains it.
func updateSignerIdentityRegexp() string {
	return "^https://github\\.com/" + regexp.QuoteMeta(updateDefaultRepo) +
		"/\\.github/workflows/release\\.yml@refs/tags/v.*$"
}

// updateVerifySignature is the in-process Sigstore verification seam. Production
// verifies the bundle against the embedded TUF trust root (no external cosign,
// no user environment dependency); tests stub it to exercise the surrounding
// fail-closed control flow without a live OIDC-signed bundle.
var updateVerifySignature = verifySigstoreBundle

// updateFetchTrustedRoot is the seam for the embedded-TUF trust-root refresh.
// Production calls sigstore-go's FetchTrustedRootWithOptions; tests inject a
// fetcher that fails (DeadlineExceeded / transport error) to drive the real
// verifySigstoreBundle classification path without a live OIDC-signed bundle.
var updateFetchTrustedRoot = root.FetchTrustedRootWithOptions

// updateTrustedRoot bootstraps the Sigstore trust root from sigstore-go's
// embedded TUF root.json, refreshing TUF metadata under a bounded HTTP client so
// the refresh honors both updateTUFRefreshTimeout and ctx cancellation (SIGINT)
// rather than hanging on http.DefaultClient with no deadline.
//
// Every failure here — the bounded refresh timing out, the parent ctx being
// cancelled, or FetchTrustedRootWithOptions returning a transport/DeadlineExceeded
// error — is wrapped in errTrustRootUnavailable so the caller classifies it as a
// retryable network condition. None of these is a signature verdict: the trust
// metadata is a network fetch, so its failure must NOT collapse into E_INTEGRITY.
func updateTrustedRoot(ctx context.Context) (*root.TrustedRoot, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, updateTUFRefreshTimeout)
	defer cancel()

	opts := tuf.DefaultOptions()
	fetcher := tuffetcher.NewDefaultFetcher()
	fetcher.SetHTTPClient(&http.Client{Timeout: updateTUFRefreshTimeout})
	opts.Fetcher = fetcher

	type result struct {
		tr  *root.TrustedRoot
		err error
	}
	done := make(chan result, 1)
	go func() {
		tr, err := updateFetchTrustedRoot(opts)
		done <- result{tr: tr, err: err}
	}()
	select {
	case <-refreshCtx.Done():
		return nil, fmt.Errorf("%w: refreshing TUF trust metadata: %w", errTrustRootUnavailable, refreshCtx.Err())
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("%w: refreshing TUF trust metadata: %w", errTrustRootUnavailable, r.err)
		}
		return r.tr, nil
	}
}

// verifySigstoreBundle verifies that artifactPath (checksums.txt) is covered by
// the Sigstore bundle at bundlePath, that the signing certificate's SAN matches
// sanRegex, that its issuer is GitHub Actions, and that the signature is logged
// in the transparency log. The trust root is bootstrapped from sigstore-go's
// embedded TUF root.json, so the trust anchor ships inside this binary rather
// than being fetched on faith. The TUF refresh is bounded by updateTrustedRoot
// (timeout + ctx cancellation) so a hung registry cannot stall verification.
func verifySigstoreBundle(ctx context.Context, artifactPath, bundlePath, sanRegex string) error {
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return fmt.Errorf("loading signature bundle: %w", err)
	}

	trustedRoot, err := updateTrustedRoot(ctx)
	if err != nil {
		return err
	}

	sev, err := verify.NewVerifier(trustedRoot,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("building sigstore verifier: %w", err)
	}

	certID, err := verify.NewShortCertificateIdentity(updateOIDCIssuer, "", "", sanRegex)
	if err != nil {
		return fmt.Errorf("building certificate identity policy: %w", err)
	}

	artifact, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("opening signed artifact: %w", err)
	}
	defer func() { _ = artifact.Close() }()

	if _, err := sev.Verify(b, verify.NewPolicy(
		verify.WithArtifact(artifact),
		verify.WithCertificateIdentity(certID),
	)); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}
