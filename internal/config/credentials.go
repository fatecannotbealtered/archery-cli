package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "archery-cli"

	// CredentialStoreKeyring stores secrets in the OS credential manager.
	CredentialStoreKeyring = "keyring"
	// CredentialStoreFile stores secrets in config.json (discouraged).
	CredentialStoreFile = "file"
)

// CredentialStore abstracts secret persistence (OS keyring or plaintext file).
type CredentialStore interface {
	// Store saves a secret value under the given service and key.
	Store(service, key, value string) error
	// Retrieve reads a secret value for the given service and key.
	Retrieve(service, key string) (string, error)
	// Delete removes a secret for the given service and key.
	Delete(service, key string) error
	// IsAvailable reports whether this store can accept read/write operations.
	IsAvailable() bool
}

// KeyringStore implements CredentialStore using the OS credential manager
// (Windows Credential Manager, macOS Keychain, Linux Secret Service).
type KeyringStore struct {
	Service string
}

// NewKeyringStore creates a KeyringStore with the default service name.
func NewKeyringStore() *KeyringStore {
	return &KeyringStore{Service: keyringService}
}

func (k *KeyringStore) Store(service, key, value string) error {
	return keyring.Set(service, key, value)
}

func (k *KeyringStore) Retrieve(service, key string) (string, error) {
	v, err := keyring.Get(service, key)
	if err != nil {
		return "", fmt.Errorf("reading OS credential store: %w", err)
	}
	return v, nil
}

func (k *KeyringStore) Delete(service, key string) error {
	return keyring.Delete(service, key)
}

func (k *KeyringStore) IsAvailable() bool {
	probe := "archery-cli-probe"
	if err := keyring.Set(k.Service, probe, "ok"); err != nil {
		return false
	}
	_ = keyring.Delete(k.Service, probe)
	return true
}

// FileStore implements CredentialStore backed by the config file (plaintext).
type FileStore struct{}

func (f *FileStore) Store(service, key, value string) error {
	// FileStore is a no-op for individual key/value pairs;
	// secrets are persisted as part of the full config Save.
	return nil
}

func (f *FileStore) Retrieve(service, key string) (string, error) {
	// FileStore retrieval is handled by reading the config file directly.
	return "", errors.New("not found in file store")
}

func (f *FileStore) Delete(service, key string) error {
	return nil
}

func (f *FileStore) IsAvailable() bool {
	return true
}

// credentialKey builds a stable keyring account name for region + username.
func credentialKey(region, username string) string {
	region = strings.TrimSpace(region)
	user := strings.TrimSpace(username)
	if user == "" {
		user = "default"
	}
	return region + "|jwt|" + user
}

// TokenStore manages JWT token persistence with keyring-first, file-fallback strategy.
type TokenStore struct {
	keyring *KeyringStore
}

// NewTokenStore creates a TokenStore that uses the OS keyring when available.
func NewTokenStore() *TokenStore {
	return &TokenStore{keyring: NewKeyringStore()}
}

// ActiveStore returns a label describing which credential store is in use.
func (ts *TokenStore) ActiveStore() string {
	if ts.keyring.IsAvailable() {
		return CredentialStoreKeyring
	}
	return CredentialStoreFile
}

// SaveTokens persists access and refresh tokens for a region.
// Tokens are stored in the keyring when available; otherwise they remain in the config file.
func (ts *TokenStore) SaveTokens(region, username, accessToken, refreshToken string) error {
	if ts.keyring.IsAvailable() {
		key := credentialKey(region, username)
		combined := accessToken + "\n" + refreshToken
		if err := ts.keyring.Store(keyringService, key, combined); err != nil {
			return fmt.Errorf("saving tokens to keyring: %w", err)
		}
		return nil
	}
	// Fallback: tokens stay in config file (caller must Save the config).
	return nil
}

// LoadTokens retrieves access and refresh tokens for a region.
// Returns empty strings if no tokens are found.
func (ts *TokenStore) LoadTokens(region, username string) (accessToken, refreshToken string, err error) {
	if ts.keyring.IsAvailable() {
		key := credentialKey(region, username)
		combined, err := ts.keyring.Retrieve(keyringService, key)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return "", "", nil
			}
			return "", "", fmt.Errorf("loading tokens from keyring: %w", err)
		}
		parts := strings.SplitN(combined, "\n", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
		return parts[0], "", nil
	}
	// Fallback: tokens are in the config file.
	return "", "", nil
}

// DeleteTokens removes persisted tokens for a region from the keyring.
func (ts *TokenStore) DeleteTokens(region, username string) error {
	if ts.keyring.IsAvailable() {
		key := credentialKey(region, username)
		if err := ts.keyring.Delete(keyringService, key); err != nil {
			// Ignore not-found errors on delete.
			return nil
		}
	}
	return nil
}

// MigrateTokensToFile moves tokens from the keyring back into the config file.
// Used when downgrading from keyring to file store.
func (ts *TokenStore) MigrateTokensToFile(region, username string) (accessToken, refreshToken string, err error) {
	return ts.LoadTokens(region, username)
}
