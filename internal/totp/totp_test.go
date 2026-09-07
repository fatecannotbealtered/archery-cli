package totp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the shared secret from RFC 6238 Appendix B: the ASCII string
// "12345678901234567890", base32-encoded. Pinning to the RFC's own vectors is
// what makes a hand-written implementation defensible — the algorithm is
// verified against the standard, not against itself.
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestGenerateAt_RFC6238Vectors(t *testing.T) {
	// RFC 6238 Appendix B, the SHA-1 rows. The RFC prints 8 digits; a 6-digit
	// code is the same value truncated, so both widths are asserted from one
	// table.
	tests := []struct {
		unix  int64
		eight string
		six   string
	}{
		{59, "94287082", "287082"},
		{1111111109, "07081804", "081804"},
		{1111111111, "14050471", "050471"},
		{1234567890, "89005924", "005924"},
		{2000000000, "69279037", "279037"},
		{20000000000, "65353130", "353130"},
	}
	for _, tc := range tests {
		at := time.Unix(tc.unix, 0).UTC()
		got8, err := GenerateAt(rfc6238Secret, at, 8)
		if err != nil {
			t.Fatalf("GenerateAt(t=%d, 8): %v", tc.unix, err)
		}
		if got8 != tc.eight {
			t.Errorf("GenerateAt(t=%d, 8) = %s, want %s", tc.unix, got8, tc.eight)
		}
		got6, err := GenerateAt(rfc6238Secret, at, 6)
		if err != nil {
			t.Fatalf("GenerateAt(t=%d, 6): %v", tc.unix, err)
		}
		if got6 != tc.six {
			t.Errorf("GenerateAt(t=%d, 6) = %s, want %s", tc.unix, got6, tc.six)
		}
	}
}

// TestGenerateAt_StableWithinStep pins the property Archery's verifier relies
// on: every instant inside one 30-second step yields the same code, and the
// next step yields a different one.
func TestGenerateAt_StableWithinStep(t *testing.T) {
	base := time.Unix(1_700_000_010, 0).UTC() // 10s into a step
	first, err := GenerateAt(rfc6238Secret, base, Digits)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0, 1, 15, 19} {
		got, err := GenerateAt(rfc6238Secret, base.Add(time.Duration(offset)*time.Second), Digits)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Errorf("code changed %ds into the same step: %s != %s", offset, got, first)
		}
	}
	next, err := GenerateAt(rfc6238Secret, base.Add(30*time.Second), Digits)
	if err != nil {
		t.Fatal(err)
	}
	if next == first {
		t.Error("code did not change across a step boundary")
	}
}

// TestGenerateAt_AcceptsUserFormatting covers how the key is actually pasted:
// authenticator apps and enrolment pages show it lowercase, grouped in fours,
// and sometimes without base32 padding.
func TestGenerateAt_AcceptsUserFormatting(t *testing.T) {
	at := time.Unix(59, 0).UTC()
	want := "287082"
	for _, form := range []string{
		rfc6238Secret,
		strings.ToLower(rfc6238Secret),
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ",
		"gezd-gnbv-gy3t-qojq-gezd-gnbv-gy3t-qojq",
		"  " + rfc6238Secret + "  ",
	} {
		got, err := GenerateAt(form, at, Digits)
		if err != nil {
			t.Fatalf("GenerateAt(%q): %v", form, err)
		}
		if got != want {
			t.Errorf("GenerateAt(%q) = %s, want %s", form, got, want)
		}
	}
}

// TestGenerateAt_UnpaddedSecret checks the padding fix-up on a secret whose
// length is not a multiple of 8 base32 characters.
func TestGenerateAt_UnpaddedSecret(t *testing.T) {
	// 26 chars -> 130 bits, needs 6 '=' to reach a multiple of 8.
	const unpadded = "JBSWY3DPEHPK3PXPJBSWY3DPEH"
	if err := Validate(unpadded); err != nil {
		t.Fatalf("Validate(unpadded): %v", err)
	}
	if _, err := GenerateAt(unpadded, time.Unix(59, 0).UTC(), Digits); err != nil {
		t.Fatalf("GenerateAt(unpadded): %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"rfc secret", rfc6238Secret, false},
		{"lowercase", strings.ToLower(rfc6238Secret), false},
		{"grouped", "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ", false},
		{"empty", "", true},
		// 1 and 8 are not in the base32 alphabet; a common transcription slip
		// for I/l and B.
		{"non base32 alphabet", "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJ1", true},
		{"looks like a code not a key", "123456", true},
		{"too short to be a key", "GEZDGNBV", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.secret)
			if tc.wantErr && err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.secret)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tc.secret, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidSecret) {
				t.Errorf("Validate(%q) error should wrap ErrInvalidSecret, got %v", tc.secret, err)
			}
		})
	}
}

// TestValidate_RejectsShortSecretBeforeStoring is the reason Validate exists at
// all: a typo must be refused at configuration time, not surface later as an
// opaque "wrong code" from Archery.
func TestValidate_RejectsShortSecretBeforeStoring(t *testing.T) {
	if err := Validate("MZXW6==="); err == nil {
		t.Error("an 8-bit secret must be rejected")
	}
}

func TestSecondsRemaining(t *testing.T) {
	tests := []struct {
		unix int64
		want int
	}{
		{0, 30},
		{1, 29},
		{29, 1},
		{30, 30},
		{59, 1},
	}
	for _, tc := range tests {
		if got := SecondsRemaining(time.Unix(tc.unix, 0).UTC()); got != tc.want {
			t.Errorf("SecondsRemaining(%d) = %d, want %d", tc.unix, got, tc.want)
		}
	}
}

func TestGenerateAt_RejectsBadDigitCount(t *testing.T) {
	for _, d := range []int{0, -1, 10} {
		if _, err := GenerateAt(rfc6238Secret, time.Unix(59, 0).UTC(), d); err == nil {
			t.Errorf("GenerateAt(digits=%d) = nil error, want error", d)
		}
	}
}
