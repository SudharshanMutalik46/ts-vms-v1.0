package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrKeyNotFound    = errors.New("key not found in keyring")
	ErrActiveKeyUnset = errors.New("active master key identifier not set or found")
)

type MasterKey struct {
	KID      string `json:"kid"`
	Material string `json:"material"` // Base64
	bytes    []byte
}

type Keyring struct {
	keys      map[string][]byte
	activeKID string
}

func NewKeyring() *Keyring {
	return &Keyring{
		keys: make(map[string][]byte),
	}
}

// LoadFromEnv loads MASTER_KEYS (JSON) and ACTIVE_MASTER_KID from environment values.
// Strict validation: Must fail if active key defaults or invalid keys found.
func (k *Keyring) LoadFromEnv() error {
	keysJSON := os.Getenv("MASTER_KEYS")
	activeKID := os.Getenv("ACTIVE_MASTER_KID")

	if keysJSON == "" {
		// If no keys defined, we can't operate.
		return errors.New("MASTER_KEYS environment variable is empty")
	}
	if activeKID == "" {
		return errors.New("ACTIVE_MASTER_KID environment variable is empty")
	}

	var rawKeys []MasterKey
	if err := json.Unmarshal([]byte(keysJSON), &rawKeys); err != nil {
		return fmt.Errorf("failed to parse MASTER_KEYS: %w", err)
	}

	k.keys = make(map[string][]byte)
	for _, rk := range rawKeys {
		if rk.KID == "" {
			return errors.New("found master key with empty KID")
		}
		if _, exists := k.keys[rk.KID]; exists {
			return fmt.Errorf("duplicate master key KID: %s", rk.KID)
		}

		decoded, err := base64.StdEncoding.DecodeString(rk.Material)
		if err != nil {
			return fmt.Errorf("invalid base64 for key %s: %w", rk.KID, err)
		}

		if len(decoded) != 32 {
			return fmt.Errorf("invalid key length for %s: expected 32 bytes (AES-256), got %d", rk.KID, len(decoded))
		}

		k.keys[rk.KID] = decoded
	}

	// Verify Active Key Exists
	if _, ok := k.keys[activeKID]; !ok {
		return fmt.Errorf("active key %s not found in MASTER_KEYS", activeKID)
	}
	k.activeKID = activeKID

	return nil
}

// WrapDEK generates a new DEK nonce, encrypts the DEK using the Active Master Key.
// Returns: masterKID, dekNonce, dekCiphertext, dekTag, err
func (k *Keyring) WrapDEK(dek []byte, aad []byte) (string, []byte, []byte, []byte, error) {
	if k.activeKID == "" {
		return "", nil, nil, nil, ErrActiveKeyUnset
	}

	masterKey, ok := k.keys[k.activeKID]
	if !ok {
		return "", nil, nil, nil, ErrActiveKeyUnset
	}

	// Encrypt DEK
	nonce, ciphertext, tag, err := EncryptGCM(masterKey, dek, aad)
	if err != nil {
		return "", nil, nil, nil, err
	}

	return k.activeKID, nonce, ciphertext, tag, nil
}

// UnwrapDEK decrypts a wrapped DEK using the specified master KID.
func (k *Keyring) UnwrapDEK(kid string, nonce, ciphertext, tag, aad []byte) ([]byte, error) {
	masterKey, ok := k.keys[kid]
	if !ok {
		return nil, ErrKeyNotFound
	}

	return DecryptGCM(masterKey, nonce, ciphertext, tag, aad)
}

// GenerateDEK creates a random 32-byte key for use as a DEK.
func GenerateDEK() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}
