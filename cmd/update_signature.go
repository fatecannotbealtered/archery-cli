package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// errTrustRootUnavailable marks a failure to obtain the Sigstore trust root.
// The trust ANCHOR ships embedded (sigstore-go's `root.json`), but refreshing
// the TUF metadata is a network step, so its failure is a retryable network
// condition — NOT a forged-release verdict. The caller classifies it as a
// network class rather than E_INTEGRITY (CLI-SPEC §14).
var errTrustRootUnavailable = errors.New("sigstore trust root unavailable")

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

// verifySigstoreBundle verifies that artifactPath (checksums.txt) is covered by
// the Sigstore bundle at bundlePath, that the signing certificate's SAN matches
// sanRegex, that its issuer is GitHub Actions, and that the signature is logged
// in the transparency log. The trust root is bootstrapped from sigstore-go's
// embedded TUF root.json, so the trust anchor ships inside this binary rather
// than being fetched on faith.
func verifySigstoreBundle(artifactPath, bundlePath, sanRegex string) error {
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return fmt.Errorf("loading signature bundle: %w", err)
	}

	trustedRoot, err := root.FetchTrustedRoot()
	if err != nil {
		// Refreshing the TUF trust metadata is a network step; mark it so the
		// caller treats it as a retryable network failure, not an integrity verdict.
		return fmt.Errorf("%w: %w", errTrustRootUnavailable, err)
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
