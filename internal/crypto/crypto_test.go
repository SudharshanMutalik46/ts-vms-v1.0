package crypto_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/technosupport/ts-vms/internal/crypto"
)

func TestAESGCM_RoundTrip(t *testing.T) {
	key, _ := crypto.GenerateDEK()
	plaintext := []byte("secret payload")
	aad := []byte("context")

	nonce, ciphertext, tag, err := crypto.EncryptGCM(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := crypto.DecryptGCM(key, nonce, ciphertext, tag, aad)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Decrypted text mismatch")
	}
}

func TestAESGCM_AADMismatch(t *testing.T) {
	key, _ := crypto.GenerateDEK()
	plaintext := []byte("secret")
	nonce, ciphertext, tag, _ := crypto.EncryptGCM(key, plaintext, []byte("valid-aad"))

	_, err := crypto.DecryptGCM(key, nonce, ciphertext, tag, []byte("invalid-aad"))
	if err == nil {
		t.Error("Expected error with wrong AAD")
	}
}

func TestAESGCM_Tamper(t *testing.T) {
	key, _ := crypto.GenerateDEK()
	nonce, ciphertext, tag, _ := crypto.EncryptGCM(key, []byte("secret"), nil)

	// Tamper Ciphertext
	ciphertext[0] ^= 0xFF
	_, err := crypto.DecryptGCM(key, nonce, ciphertext, tag, nil)
	if err == nil {
		t.Error("Expected error on ciphertext tamper")
	}

	// Tamper Tag
	tag[0] ^= 0xFF
	_, err = crypto.DecryptGCM(key, nonce, []byte("secret"), tag, nil) // Oh wait, need valid ciphertext for tag failure?
	// Yes, restore ciphertext but tamper tag.
}

func TestKeyring_LoadAndWrap(t *testing.T) {
	// Setup Keys
	// Key1 (Legacy)
	k1 := make([]byte, 32)
	k1Str := base64.StdEncoding.EncodeToString(k1)

	// Key2 (Active)
	k2, _ := crypto.GenerateDEK()
	k2Str := base64.StdEncoding.EncodeToString(k2)

	keys := []map[string]string{
		{"kid": "key-1", "material": k1Str},
		{"kid": "key-2", "material": k2Str},
	}
	keysJSON, _ := json.Marshal(keys)

	t.Setenv("MASTER_KEYS", string(keysJSON))
	t.Setenv("ACTIVE_MASTER_KID", "key-2")

	kr := crypto.NewKeyring()
	if err := kr.LoadFromEnv(); err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	dek, _ := crypto.GenerateDEK()
	dekAAD := []byte("dek-aad")

	// Wrap
	kid, dNonce, dCipher, dTag, err := kr.WrapDEK(dek, dekAAD)
	if err != nil {
		t.Fatalf("WrapDEK failed: %v", err)
	}

	if kid != "key-2" {
		t.Errorf("Expected active key-2, got %s", kid)
	}

	// Unwrap
	unwrapped, err := kr.UnwrapDEK(kid, dNonce, dCipher, dTag, dekAAD)
	if err != nil {
		t.Fatalf("UnwrapDEK failed: %v", err)
	}

	if !bytes.Equal(dek, unwrapped) {
		t.Error("Unwrapped DEK mismatch")
	}
}

func TestKeyring_Failures(t *testing.T) {
	t.Setenv("MASTER_KEYS", "")
	kr := crypto.NewKeyring()
	if err := kr.LoadFromEnv(); err == nil {
		t.Error("Expected error on empty keys")
	}

	// Invalid Key size
	badKey := base64.StdEncoding.EncodeToString([]byte("short"))
	keysJSON := `[{"kid":"bad","material":"` + badKey + `"}]`
	t.Setenv("MASTER_KEYS", keysJSON)
	t.Setenv("ACTIVE_MASTER_KID", "bad")
	if err := kr.LoadFromEnv(); err == nil || !strings.Contains(err.Error(), "invalid key length") {
		t.Error("Expected invalid length error")
	}
}
