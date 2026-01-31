package license

import (
	"time"

	"github.com/google/uuid"
)

// Status Enum matching Phase 1.6 requirements strictly
type Status string

const (
	StatusValid            Status = "VALID"
	StatusExpiredGrace     Status = "EXPIRED_GRACE"
	StatusExpiredBlocked   Status = "EXPIRED_BLOCKED"
	StatusInvalidSignature Status = "INVALID_SIGNATURE"
	StatusMissing          Status = "MISSING"
	StatusParseError       Status = "PARSE_ERROR"
	// Internal helper if needed, but keeping strict to requirements
)

// LicenseFile represents the on-disk JSON structure
type LicenseFile struct {
	PayloadB64 string `json:"payload_b64"`
	SigB64     string `json:"sig_b64"`
	Alg        string `json:"alg"` // Expected: RS256
}

// LicensePayload represents the business logic content
type LicensePayload struct {
	LicenseID    uuid.UUID       `json:"license_id"`
	CustomerName string          `json:"customer_name"` // PII - Do not log
	TenantScope  string          `json:"tenant_scope"`  // "all" or specific UUID
	IssuedAt     time.Time       `json:"issued_at_utc"`
	ValidUntil   time.Time       `json:"valid_until_utc"`
	Limits       LicenseLimits   `json:"limits"`
	Features     map[string]bool `json:"features"`
}

type LicenseLimits struct {
	MaxCameras int `json:"max_cameras"`
	MaxNVRs    int `json:"max_nvrs"`
}

// LicenseState serves as the in-memory representation
type LicenseState struct {
	Status       Status
	Payload      *LicensePayload
	LastReload   time.Time
	DaysToExpiry int
	ReasonCode   string
}
