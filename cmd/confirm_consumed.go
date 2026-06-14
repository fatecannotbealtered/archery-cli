package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Single-use confirm tokens: once a confirm token has been accepted to execute a
// write, its fingerprint is recorded so the SAME token cannot drive a second
// write. archery has no reliable upstream resource version to bind to, so
// single-use is the safe-retry mechanism here — a confirmed write that times out
// cannot be blindly replayed; the retry is rejected with E_CONFLICT and the agent
// must re-run --dry-run (which reveals the now-current state). The store lives at
// ~/.archery-cli/confirm-consumed.json (0600) and is pruned of expired entries on
// every access.
var consumedTokensMu sync.Mutex

func consumedTokensPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", err
	}
	return filepath.Join(home, "."+updateBinaryName, "confirm-consumed.json"), nil
}

// tokenFingerprint is a short, non-reversible id for a confirm token: the first
// 16 hex chars of sha256(token).
func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16]
}

// confirmTokenExpiryUnix extracts the expiry unix seconds from a ct_<unix>_<hex>
// token, or 0 if it cannot be parsed (the consumed-token entry then prunes on the
// next access).
func confirmTokenExpiryUnix(token string) int64 {
	parts := strings.Split(token, "_")
	if len(parts) != 3 || parts[0] != "ct" {
		return 0
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func loadConsumedTokens(path string, now time.Time) map[string]int64 {
	out := map[string]int64{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var stored map[string]int64
	if json.Unmarshal(data, &stored) != nil {
		return out
	}
	// Drop expired entries so the file cannot grow without bound.
	for fp, exp := range stored {
		if exp > now.Unix() {
			out[fp] = exp
		}
	}
	return out
}

// isConfirmTokenConsumed reports whether this token has already been used.
func isConfirmTokenConsumed(token string, now time.Time) bool {
	path, err := consumedTokensPath()
	if err != nil {
		return false // cannot check; do not block the operation
	}
	consumedTokensMu.Lock()
	defer consumedTokensMu.Unlock()
	tokens := loadConsumedTokens(path, now)
	_, ok := tokens[tokenFingerprint(token)]
	return ok
}

// markConfirmTokenConsumed records the token as used until its expiry. Best
// effort: a storage failure does not block the write (single-use simply cannot be
// guaranteed on that host).
func markConfirmTokenConsumed(token string, expiresUnix int64, now time.Time) {
	path, err := consumedTokensPath()
	if err != nil {
		return
	}
	consumedTokensMu.Lock()
	defer consumedTokensMu.Unlock()
	tokens := loadConsumedTokens(path, now)
	tokens[tokenFingerprint(token)] = expiresUnix
	data, err := json.Marshal(tokens)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}
