package media

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/nvr/adapters"
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
	probeErr := adapters.ProbeRTSPWithTimeout(context.Background(), targetURL, ValidationTimeout)
	rtt := int(time.Since(start).Milliseconds())

	if probeErr == nil {
		return ValidationResult{Status: StatusValid, RTT: rtt}
	}

	errText := strings.ToLower(probeErr.Error())

	if strings.Contains(errText, "timeout") {
		return ValidationResult{Status: StatusTimeout, LastErrorCode: "probe_timeout", RTT: rtt}
	}

	if strings.Contains(errText, "auth_failed") || strings.Contains(errText, "401") || strings.Contains(errText, "403") {
		return ValidationResult{Status: StatusUnauthorized, LastErrorCode: "401_unauthorized", RTT: rtt}
	}

	if strings.Contains(errText, "404") {
		return ValidationResult{Status: StatusInvalid, LastErrorCode: "404_not_found", RTT: rtt}
	}

	return ValidationResult{Status: StatusError, LastErrorCode: errText, RTT: rtt}
}

func SanitizeRTSPURL(raw string) string {
	// 1. First unescape XML entities like &amp;
	unescaped := html.UnescapeString(raw)

	// 2. Remove credentials manually to avoid url.Parse/String() double-encoding side effects
	// rtsp://user:pass@host:port/path
	if idx := strings.Index(unescaped, "://"); idx != -1 {
		proto := unescaped[:idx+3]
		rest := unescaped[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			// Find first slash to ensure @ is in authority section
			slash := strings.Index(rest, "/")
			if slash == -1 || at < slash {
				return proto + rest[at+1:]
			}
		}
	}

	return unescaped
}
