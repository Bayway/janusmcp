// Package vault stores per-account secrets securely.
//
// Two backends:
//   - OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service)
//     via github.com/zalando/go-keyring — the default and recommended one;
//   - an encrypted file (AES-256-GCM) as a headless fallback for CI / servers
//     without a Secret Service, keyed by a local key file.
//
// Secrets are referenced from config as "vault:<name>" and never stored in plaintext config.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const service = "janusmcp"

// Vault is the storage interface used by the broker.
type Vault interface {
	Get(name string) (string, error)
	Set(name, secret string) error
	Delete(name string) error
}

// ParseRef extracts <name> from a "vault:<name>" reference.
func ParseRef(ref string) (string, bool) {
	if strings.HasPrefix(ref, "vault:") {
		return strings.TrimPrefix(ref, "vault:"), true
	}
	return "", false
}

// Resolve returns a SecretResolver-compatible func for config.Load.
func Resolve(v Vault) func(string) (string, error) {
	return func(ref string) (string, error) {
		name, ok := ParseRef(ref)
		if !ok {
			return ref, nil
		}
		return v.Get(name)
	}
}

// ---------------------------------------------------------------------------
// OS keychain backend
// ---------------------------------------------------------------------------

type keyringVault struct{}

// NewKeyring returns the OS keychain-backed vault.
func NewKeyring() Vault { return keyringVault{} }

func (keyringVault) Get(name string) (string, error) {
	s, err := keyring.Get(service, name)
	if err != nil {
		return "", fmt.Errorf("keyring get %q: %w", name, err)
	}
	return s, nil
}
func (keyringVault) Set(name, secret string) error { return keyring.Set(service, name, secret) }
func (keyringVault) Delete(name string) error      { return keyring.Delete(service, name) }

// ---------------------------------------------------------------------------
// Encrypted-file fallback backend (AES-256-GCM)
// ---------------------------------------------------------------------------

type fileVault struct {
	path string
	key  []byte
	mu   sync.Mutex
}

// NewFile returns a file-backed vault. The 32-byte key is read from keyPath,
// generated on first use if absent (chmod 0600).
func NewFile(path, keyPath string) (Vault, error) {
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &fileVault{path: path, key: key}, nil
}

func loadOrCreateKey(keyPath string) ([]byte, error) {
	if b, err := os.ReadFile(keyPath); err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("key file %s must be 32 bytes, got %d", keyPath, len(b))
		}
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}
	return key, nil
}

func (f *fileVault) readAll() (map[string]string, error) {
	m := map[string]string{}
	enc, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	if len(enc) == 0 {
		return m, nil
	}
	block, err := aes.NewCipher(f.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(enc) < ns {
		return nil, fmt.Errorf("vault file corrupt")
	}
	nonce, ct := enc[:ns], enc[ns:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("vault decrypt: %w", err)
	}
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (f *fileVault) writeAll(m map[string]string) error {
	plain, err := json.Marshal(m)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(f.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, plain, nil)
	return os.WriteFile(f.path, ct, 0o600)
}

func (f *fileVault) Get(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.readAll()
	if err != nil {
		return "", err
	}
	v, ok := m[name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", name)
	}
	return v, nil
}

func (f *fileVault) Set(name, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.readAll()
	if err != nil {
		return err
	}
	m[name] = secret
	return f.writeAll(m)
}

func (f *fileVault) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.readAll()
	if err != nil {
		return err
	}
	delete(m, name)
	return f.writeAll(m)
}
