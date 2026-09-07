// Package totp derives RFC 6238 time-based one-time passwords from a shared
// secret, so an unattended run can complete Archery's 2FA without a human
// typing a code.
//
// It is implemented on the standard library rather than pulled from a module.
// The algorithm is HMAC-SHA1 plus dynamic truncation — small enough to read in
// one sitting and pinned here by the RFC's own test vectors — while the usual
// Go TOTP package also carries a QR-code encoder this tool never uses. Fewer
// modules is also fewer advisories to chase in the fail-closed govulncheck gate.
//
// Defaults match Archery's server side, which is pyotp.TOTP(secret) with its
// defaults: SHA-1, 6 digits, a 30-second step counted from the Unix epoch.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// Period is the TOTP step in seconds (RFC 6238 recommends, and pyotp
	// defaults to, 30).
	Period = 30
	// Digits is the generated code length.
	Digits = 6
)

// ErrInvalidSecret reports a secret that is not usable base32. It is returned
// at configuration time so a typo is refused before it is stored, rather than
// surfacing much later as an opaque "wrong code" from the server.
var ErrInvalidSecret = errors.New("invalid TOTP secret: expected base32 (the key behind the enrolment QR code, A-Z and 2-7)")

// normalize prepares a user-supplied secret for decoding.
//
// Authenticator apps and enrolment pages present the key in lowercase, in
// space-separated groups of four, and sometimes without the trailing base32
// padding. All three are the same secret, so all three are accepted.
func normalize(secret string) string {
	s := strings.ToUpper(strings.TrimSpace(secret))
	s = strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(s)
	if pad := len(s) % 8; pad != 0 {
		s += strings.Repeat("=", 8-pad)
	}
	return s
}

// Validate reports whether secret decodes as base32 and is long enough to be a
// real key. It performs no network or clock access.
func Validate(secret string) error {
	key, err := base32.StdEncoding.DecodeString(normalize(secret))
	if err != nil {
		return ErrInvalidSecret
	}
	// RFC 4226 §4 R6 requires at least 128 bits of shared secret; Archery's
	// generate_key uses pyotp.random_base32(32), which is 160 bits.
	if len(key) < 16 {
		return fmt.Errorf("%w: decoded to %d bytes, need at least 16", ErrInvalidSecret, len(key))
	}
	return nil
}

// GenerateAt returns the code for secret at time t.
//
// Exported separately from Generate so tests can pin behaviour to the RFC's
// vectors instead of to whatever the wall clock says while they run.
func GenerateAt(secret string, t time.Time, digits int) (string, error) {
	key, err := base32.StdEncoding.DecodeString(normalize(secret))
	if err != nil {
		return "", ErrInvalidSecret
	}
	if digits <= 0 || digits > 9 {
		return "", fmt.Errorf("unsupported digit count %d", digits)
	}

	counter := uint64(t.Unix() / Period)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.4): the low nibble of the last byte picks
	// the 4-byte window, whose top bit is masked off so the value is positive
	// regardless of the platform's integer width.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	code := value % uint32(math.Pow10(digits))
	return fmt.Sprintf("%0*d", digits, code), nil
}

// Generate returns the current 6-digit code for secret.
func Generate(secret string) (string, error) {
	return GenerateAt(secret, time.Now(), Digits)
}

// SecondsRemaining reports how long the current code stays valid.
//
// A derived code inherits the same ~30-second window as a human-typed one, so
// callers that are about to spend time elsewhere before sending it can use this
// to decide whether to derive again.
func SecondsRemaining(t time.Time) int {
	return Period - int(t.Unix()%Period)
}
