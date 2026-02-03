package media

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	WorkerPoolSize    = 5
	QueueSize         = 100
	ValidationTimeout = 5 * time.Second
)

type ValidationStatus string

const (
	StatusValid              ValidationStatus = "valid"
	StatusInvalid            ValidationStatus = "invalid"
	StatusUnauthorized       ValidationStatus = "unauthorized"
	StatusMissingCredentials ValidationStatus = "missing_credentials"
	StatusTimeout            ValidationStatus = "timeout"
	StatusRTSP_URIMissing    ValidationStatus = "rtsp_uri_missing"
	StatusUnsupportedCodec   ValidationStatus = "unsupported_codec" // If we added that check
	StatusError              ValidationStatus = "error"
)

type ValidationResult struct {
	Status        ValidationStatus
	LastErrorCode string
	RTT           int // ms
}

type ValidationJob struct {
	TenantID uuid.UUID
	CameraID uuid.UUID
	Variant  string // main/sub
	RTSPURL  string
	Username string
	Password string
}

type Validator struct {
	jobs    chan ValidationJob
	results chan jobResult
	// Dedup map
	mu      sync.Mutex
	pending map[string]bool // key: cameraID:variant

	// Callback for persistence
	OnResult func(job ValidationJob, res ValidationResult)
}

type jobResult struct {
	Job ValidationJob
	Res ValidationResult
}

func NewValidator(onResult func(ValidationJob, ValidationResult)) *Validator {
	v := &Validator{
		jobs:     make(chan ValidationJob, QueueSize),
		results:  make(chan jobResult, QueueSize),
		pending:  make(map[string]bool),
		OnResult: onResult,
	}
	// Start workers
	for i := 0; i < WorkerPoolSize; i++ {
		go v.worker()
	}
	// Start result processor
	go v.resultProcessor()
	return v
}

func (v *Validator) Enqueue(job ValidationJob) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	key := fmt.Sprintf("%s:%s", job.CameraID, job.Variant)
	if v.pending[key] {
		return false // Already queued
	}

	select {
	case v.jobs <- job:
		v.pending[key] = true
		return true
	default:
		// Queue full, drop or evict? User said "Bounded queue (drop/replace old)".
		// Simplest Bounded: Drop new if full.
		// Metrics should track dropped.
		return false
	}
}

func (v *Validator) worker() {
	for job := range v.jobs {
		res := v.validate(job)
		v.results <- jobResult{Job: job, Res: res}
	}
}

func (v *Validator) resultProcessor() {
	for r := range v.results {
		v.mu.Lock()
		key := fmt.Sprintf("%s:%s", r.Job.CameraID, r.Job.Variant)
		delete(v.pending, key)
		v.mu.Unlock()

		if v.OnResult != nil {
			v.OnResult(r.Job, r.Res)
		}
	}
}

func (v *Validator) validate(job ValidationJob) ValidationResult {
	if job.RTSPURL == "" {
		return ValidationResult{Status: StatusRTSP_URIMissing, LastErrorCode: "empty_url"}
	}

	// Inject Credentials into URL for connection test
	targetURL := job.RTSPURL
	if job.Username != "" {
		// Insert user:pass if scheme is rtsp://
		if strings.HasPrefix(targetURL, "rtsp://") {
			targetURL = strings.Replace(targetURL, "rtsp://", fmt.Sprintf("rtsp://%s:%s@", url.QueryEscape(job.Username), url.QueryEscape(job.Password)), 1)
		}
	} else {
		// If credentials missing but might be required?
		// We proceed. If it fails 401, we mark Unauthorized.
		// If app logic KNEW it required creds, we could short circuit.
		// But let's verify connectivity.
	}

	start := time.Now()

	// TCP Connect (Stage 1)
	u, err := url.Parse(targetURL)
	if err != nil {
		return ValidationResult{Status: StatusInvalid, LastErrorCode: "parse_error"}
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":554" // Default RTSP port
	}

	conn, err := net.DialTimeout("tcp", host, ValidationTimeout)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			return ValidationResult{Status: StatusTimeout, LastErrorCode: "tcp_timeout"}
		}
		return ValidationResult{Status: StatusError, LastErrorCode: err.Error()}
	}
	defer conn.Close()

	// RTSP OPTIONS (Stage 2)
	// Simple handshake
	// CSeq: 1
	// User-Agent: TechnoSupportVMS

	req := fmt.Sprintf("OPTIONS %s RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: TechnoSupportVMS\r\n\r\n", job.RTSPURL) // Use raw URL in request line usually, or full
	// Auth is tricky in raw socket. If 401, we need to handle Digest/Basic.
	// For CONNECTIVITY test (validity), getting a 401 implies "Valid RTSP Endpoint" but "Unauthorized".
	// Getting 200 OK implies "Valid and Authorized" (or no auth).
	// So 401 is actually a success for "Connectivity", but failure for "Streamability".
	// We map 401 to StatusUnauthorized.

	conn.SetDeadline(time.Now().Add(ValidationTimeout))
	if _, err := conn.Write([]byte(req)); err != nil {
		return ValidationResult{Status: StatusError, LastErrorCode: "write_error"}
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return ValidationResult{Status: StatusInvalid, LastErrorCode: "read_error"}
	}

	resp := string(buf[:n])
	rtt := int(time.Since(start).Milliseconds())

	if strings.HasPrefix(resp, "RTSP/1.0 200 OK") {
		return ValidationResult{Status: StatusValid, RTT: rtt}
	}

	if strings.Contains(resp, "401 Unauthorized") {
		return ValidationResult{Status: StatusUnauthorized, LastErrorCode: "401_unauthorized", RTT: rtt}
	}

	if strings.Contains(resp, "404 Not Found") {
		return ValidationResult{Status: StatusInvalid, LastErrorCode: "404_not_found", RTT: rtt}
	}

	return ValidationResult{Status: StatusInvalid, LastErrorCode: "unknown_response", RTT: rtt}
}

// Helper: Sanitize URL
func SanitizeRTSPURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw // fallback
	}
	u.User = nil
	return u.String()
}
