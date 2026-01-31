package license

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
)

// MaxLicenseSizeBytes - 64KB Bound
const MaxLicenseSizeBytes = 64 * 1024

// Parser handles the cryptographic verification and decoding
type Parser struct {
	PublicKey *rsa.PublicKey
}

func NewParser(pubKeyPath string) (*Parser, error) {
	// Load Key
	data, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %v", err)
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		// Try parsing as RSA PUBLIC KEY if generic fails, or error
		if block != nil && block.Type == "RSA PUBLIC KEY" {
			// ok
		} else {
			return nil, fmt.Errorf("failed to decode PEM block containing public key")
		}
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		// Fallback for PKCS1
		if pkcs1Pub, err2 := x509.ParsePKCS1PublicKey(block.Bytes); err2 == nil {
			pub = pkcs1Pub
		} else {
			return nil, fmt.Errorf("failed to parse public key: %v", err)
		}
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not RSA")
	}

	return &Parser{PublicKey: rsaPub}, nil
}

// ParseAndVerify reads file, checks bounds, parses JSON, verifies signature
func (p *Parser) ParseAndVerify(path string) (*LicensePayload, Status, error) {
	// 1. Read File with Bound
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, StatusMissing, nil
	}
	if info.Size() > MaxLicenseSizeBytes {
		return nil, StatusParseError, fmt.Errorf("file too large")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, StatusParseError, err
	}

	// 2. Parse Outer JSON
	var lf LicenseFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, StatusParseError, fmt.Errorf("malformed license file")
	}

	// Base64 Decode
	payloadBytes, err := base64.StdEncoding.DecodeString(lf.PayloadB64)
	if err != nil {
		return nil, StatusParseError, fmt.Errorf("malformed payload b64")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(lf.SigB64)
	if err != nil {
		return nil, StatusParseError, fmt.Errorf("malformed sig b64")
	}

	// 3. Verify Signature (RS256)
	hashed := sha256.Sum256(payloadBytes)
	err = rsa.VerifyPKCS1v15(p.PublicKey, crypto.SHA256, hashed[:], sigBytes)
	if err != nil {
		return nil, StatusInvalidSignature, fmt.Errorf("signature verification failed")
	}

	// 4. Parse Payload
	var payload LicensePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, StatusParseError, fmt.Errorf("malformed payload json")
	}

	return &payload, StatusValid, nil
}
