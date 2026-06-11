package cmd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	confirmSecretOnce  sync.Once
	confirmSecretValue []byte
)

// confirmSecret returns the machine-local key that signs confirm tokens, so a
// token cannot be fabricated by recomputing a public hash: it must come from a
// real --dry-run on this machine. Created on first use at
// ~/.archery-cli/confirm.secret with 0600 permissions.
func confirmSecret() []byte {
	confirmSecretOnce.Do(func() {
		confirmSecretValue = loadOrCreateConfirmSecret()
	})
	return confirmSecretValue
}

func loadOrCreateConfirmSecret() []byte {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		warnConfirmSecretFallback("cannot resolve home directory")
		return nil
	}
	path := filepath.Join(home, "."+updateBinaryName, "confirm.secret")
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		warnConfirmSecretFallback("cannot generate random key")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		warnConfirmSecretFallback("cannot create config directory")
		return nil
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		warnConfirmSecretFallback("cannot persist key file")
		return nil
	}
	return secret
}

func warnConfirmSecretFallback(reason string) {
	fmt.Fprintf(os.Stderr, "warning: %s; confirm tokens fall back to unkeyed hashing\n", reason)
}

// confirmDigest32 is a drop-in replacement for sha256.Sum256 on token seeds,
// keyed with the machine-local secret when available.
func confirmDigest32(data []byte) [32]byte {
	secret := confirmSecret()
	if len(secret) == 0 {
		return sha256.Sum256(data)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}
