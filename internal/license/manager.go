package license

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
)

type Manager struct {
	mu           sync.RWMutex
	state        LicenseState
	parser       *Parser
	usage        UsageProvider
	path         string
	auditService *audit.Service // For reload events
}

func NewManager(path string, parser *Parser, usage UsageProvider, audit *audit.Service) *Manager {
	m := &Manager{
		path:         path,
		parser:       parser,
		usage:        usage,
		auditService: audit,
		state:        LicenseState{Status: StatusMissing, ReasonCode: "init"},
	}
	m.Reload() // Initial Load
	return m
}

// Reload re-reads file, verifies, updates state atomically
func (m *Manager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	payload, status, err := m.parser.ParseAndVerify(m.path)

	// Pre-Audit preparation
	auditPayload := audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "license.reload",
		TargetType: "license",
		TargetID:   m.path,
		CreatedAt:  time.Now(),
		// TenantID? License reload is system-wide usually, or default tenant?
		// Audit requires TenantID. Use Null/System UUID or default.
		// Let's assume system level 0000...
		TenantID: uuid.MustParse("00000000-0000-0000-0000-000000000000"),
	}

	if err != nil {
		m.state = LicenseState{
			Status:     status,
			ReasonCode: err.Error(),
			LastReload: time.Now(),
		}
		if m.auditService != nil {
			go m.auditService.WriteEvent(context.Background(), auditPayload)
		}
		return
	}

	if payload == nil {
		m.state = LicenseState{
			Status:     status,
			ReasonCode: "payload_missing",
			LastReload: time.Now(),
		}
		if m.auditService != nil {
			auditPayload.Result = "failure"
			auditPayload.ReasonCode = string(status)
			go m.auditService.WriteEvent(context.Background(), auditPayload)
		}
		return
	}

	// 5. Check Time Validity (Logic for Grace)
	now := time.Now().UTC()
	finalStatus := StatusValid
	daysToExpiry := 0

	if now.Before(payload.IssuedAt) {
		finalStatus = StatusValid // Or maybe Invalid if strictly enforcing? Prompt says "rejected".
		// Wait, Prompt 8) "not-yet-valid license rejected".
		// Let's handle Future Issue Date.
		m.state = LicenseState{
			// Let's treat "Issued in Future" as PARSE_ERROR (logic fail).
			Status:     StatusParseError,
			ReasonCode: "future_issue_date",
			LastReload: time.Now(),
		}
		return
	}

	if now.After(payload.ValidUntil) {
		diff := now.Sub(payload.ValidUntil)
		days := int(diff.Hours() / 24)
		daysToExpiry = -days

		if days <= 30 {
			finalStatus = StatusExpiredGrace
		} else {
			finalStatus = StatusExpiredBlocked
		}
	} else {
		diff := payload.ValidUntil.Sub(now)
		daysToExpiry = int(diff.Hours() / 24)
	}

	m.state = LicenseState{
		Status:       finalStatus,
		Payload:      payload,
		LastReload:   time.Now(),
		DaysToExpiry: daysToExpiry,
	}

	// Emit Audit Success
	auditPayload.Result = "success"
	if m.auditService != nil {
		go m.auditService.WriteEvent(context.Background(), auditPayload)
	}
}

// GetState returns copy safe for reading
func (m *Manager) GetState() LicenseState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return by value (shallow copy of struct, pointer to Payload shared but read-only intnent)
	return m.state
}

// CheckOperation checks if an operation is allowed
func (m *Manager) CheckOperation(op string, tenantID uuid.UUID) error {
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()

	// 1. Check Base Status
	switch state.Status {
	case StatusMissing, StatusParseError, StatusInvalidSignature:
		return fmt.Errorf("license_invalid")
	case StatusExpiredBlocked:
		// Deny All
		return fmt.Errorf("license_expired_blocked")
	case StatusExpiredGrace:
		// Deny "create" ops (Capacity Increase)
		if isCapacityOp(op) {
			return fmt.Errorf("license_expired_grace")
		}
		// Allow "view" ops
	case StatusValid:
		// Allow
	}

	// 2. Check Limits (if valid or grace-view)
	if state.Payload == nil {
		return fmt.Errorf("license_invalid")
	}

	// Example: Camera Create Limit
	if op == "camera.create" {
		usage, err := m.usage.CurrentUsage(context.Background(), tenantID)
		if err != nil {
			return err // Fail safe?
		}
		if usage.Cameras >= state.Payload.Limits.MaxCameras {
			return fmt.Errorf("limit_exceeded")
		}
	}

	// Example: Feature Flag
	// key: feature.use.<name>
	// if prefix "feature.use."
	// check map

	return nil
}

func isCapacityOp(op string) bool {
	return op == "camera.create" || op == "nvr.create"
}
