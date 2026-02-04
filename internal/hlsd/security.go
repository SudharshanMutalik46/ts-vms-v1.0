package hlsd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid hls token")
	ErrExpiredToken = errors.New("hls token expired")
)

// KeyProvider facilitates kid-based secret lookup
type KeyProvider interface {
	GetKey(kid string) ([]byte, error)
}

// MapKeyProvider is a simple implementation of KeyProvider
type MapKeyProvider struct {
	Keys map[string][]byte
}

func (p *MapKeyProvider) GetKey(kid string) ([]byte, error) {
	key, ok := p.Keys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
	return key, nil
}

// ValidateHLSToken validates the HMAC token based on the Phase 3.2 contract.
// Expected canonical string: hls|{sub}|{sid}|{exp}
func ValidateHLSToken(cameraID, sessionID string, query url.Values, keys KeyProvider) error {
	sub := query.Get("sub")
	sid := query.Get("sid")
	expStr := query.Get("exp")
	scope := query.Get("scope")
	kid := query.Get("kid")
	sigHex := query.Get("sig")

	if sub != cameraID || sid != sessionID || scope != "hls" || kid == "" || sigHex == "" {
		fmt.Printf("[DEBUG] Token Validation Validation Failed:\nSUB: '%s' vs '%s'\nSID: '%s' vs '%s'\nSCOPE: '%s'\nKID: '%s'\nSIG: '%s'\n", sub, cameraID, sid, sessionID, scope, kid, sigHex)
		return ErrInvalidToken
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		fmt.Printf("[DEBUG] Token Validation: Exp Parse Error: %v\n", err)
		return ErrInvalidToken
	}

	if time.Now().Unix() > exp {
		fmt.Printf("[DEBUG] Token Validation: Expired! Now: %d, Exp: %d\n", time.Now().Unix(), exp)
		return ErrExpiredToken
	}

	key, err := keys.GetKey(kid)
	if err != nil {
		fmt.Printf("[DEBUG] Token Validation: Key Lookup Failed for kid '%s': %v\n", kid, err)
		return ErrInvalidToken
	}

	// canonical string: hls|{sub}|{sid}|{exp}
	canonical := fmt.Sprintf("hls|%s|%s|%s", sub, sid, expStr)

	h := hmac.New(sha256.New, key)
	h.Write([]byte(canonical))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(sigHex), []byte(expectedSig)) {
		fmt.Printf("[DEBUG] HMAC Mismatch!\nCanonical: %s\nExpected: %s\nReceived: %s\nKey: %s\n", canonical, expectedSig, sigHex, "REDACTED")
		return ErrInvalidToken
	}

	return nil
}

// Sign helper for tests and token generation
func Sign(data string, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
