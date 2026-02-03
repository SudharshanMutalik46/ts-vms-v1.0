package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

var (
	ErrInvalidKeySize = errors.New("invalid key size: must be 32 bytes for AES-256")
	ErrDecryption     = errors.New("decryption failed: invalid key, tag, or context")
)

// EncryptGCM encrypts plaintext using AES-256-GCM with the given key and AAD.
// Returns nonce, ciphertext, and tag.
// Note: Go's GCM Seal appends the tag to the ciphertext.
// We will separate them in the return values to match the schema storage requirements if needed,
// OR keep them combined if the schema prefers. The schema has columns: dek_ciphertext, dek_tag.
// So we should separate them.
func EncryptGCM(key []byte, plaintext []byte, aad []byte) (nonce, ciphertext, tag []byte, err error) {
	if len(key) != 32 {
		return nil, nil, nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, err
	}

	// Standard nonce size is 12 bytes
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, nil, err
	}

	// Seal appends tag to ciphertext.
	// dst = nonce (optional prefix) + ciphertext + tag.
	// We want just ciphertext + tag, then split.
	full := gcm.Seal(nil, nonce, plaintext, aad)

	// Split Tag (last 16 bytes usually for GCM standard)
	tagSize := gcm.Overhead()
	if len(full) < tagSize {
		return nil, nil, nil, errors.New("encryption error: output too short")
	}

	ciphertext = full[:len(full)-tagSize]
	tag = full[len(full)-tagSize:]

	return nonce, ciphertext, tag, nil
}

// DecryptGCM decrypts ciphertext using AES-256-GCM.
// Expects explicit nonce and tag separate from ciphertext.
func DecryptGCM(key, nonce, ciphertext, tag, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}

	// Reassemble for Open: ciphertext + tag
	full := make([]byte, len(ciphertext)+len(tag))
	copy(full, ciphertext)
	copy(full[len(ciphertext):], tag)

	plaintext, err := gcm.Open(nil, nonce, full, aad)
	if err != nil {
		// Generic error to avoid leakage, but logged internally?
		// "cipher: message authentication failed" is standard Go error.
		// We return generic to caller.
		return nil, ErrDecryption
	}

	return plaintext, nil
}
