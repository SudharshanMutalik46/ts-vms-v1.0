package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type AuditHandler struct {
	Service *audit.Service
	Perms   *middleware.PermissionMiddleware
}

// AuditEvent matches the desktop's AuditEvent model
type AuditEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Result    string    `json:"result"`
	Details   string    `json:"details"`
	ClientIP  string    `json:"client_ip"`
}

func (h *AuditHandler) GetEvents(w http.ResponseWriter, r *http.Request) {
	// 1. RBAC & Tenant Context
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Build Filter
	filter := audit.AuditFilter{
		TenantID: uuid.MustParse(ac.TenantID),
		Limit:    100, // Default limit
	}

	// 3. Query Service
	events, _, err := h.Service.QueryEvents(r.Context(), filter)
	if err != nil {
		http.Error(w, "Failed to query events", http.StatusInternalServerError)
		return
	}

	// 4. Map to API Model
	apiEvents := make([]AuditEvent, 0, len(events))
	for _, e := range events {
		actor := "system"
		if e.ActorDisplayName != "" {
			actor = e.ActorDisplayName
		} else if e.ActorUserID != nil {
			actor = e.ActorUserID.String()
		}

		details := ""
		if len(e.Metadata) > 0 {
			details = string(e.Metadata)
		} else if e.ReasonCode != "" {
			details = e.ReasonCode
		}

		apiEvents = append(apiEvents, AuditEvent{
			ID:        e.EventID.String(),
			Timestamp: e.CreatedAt,
			Actor:     actor,
			Action:    e.Action,
			Resource:  fmt.Sprintf("%s:%s", e.TargetType, e.TargetID),
			Result:    e.Result,
			Details:   details,
			ClientIP:  e.ClientIP,
		})
	}

	// 5. Response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(apiEvents)
}

// AuditExportRequest defines the body for export
type AuditExportRequest struct {
	Format    string     `json:"format"` // csv, jsonl
	StartTime *time.Time `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
}

func (h *AuditHandler) ExportEvents(w http.ResponseWriter, r *http.Request) {
	// RBAC: audit.export
	// Tenant Isolation
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse Request Body
	var req AuditExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If body is empty or invalid, default to no filters, jsonl
		// But let's log it
		fmt.Printf("Export request decode error (using defaults): %v\n", err)
	}

	filter := audit.AuditFilter{
		TenantID: uuid.MustParse(ac.TenantID),
		DateFrom: req.StartTime,
		DateTo:   req.EndTime,
	}

	// Audit the Export Action (Async)
	// We do this before streaming so we capture the attempt even if stream fails mid-way (though request is valid)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		userID := uuid.Nil
		if uid, err := uuid.Parse(ac.UserID); err == nil {
			userID = uid
		}

		h.Service.WriteEvent(ctx, audit.AuditEvent{
			ID:          uuid.New(), // Service might overwrite or use this
			EventID:     uuid.New(),
			TenantID:    uuid.MustParse(ac.TenantID),
			ActorUserID: &userID,
			Action:      "audit.export",
			TargetType:  "audit_log",
			TargetID:    "export",  // Conceptually the export itself
			Result:      "success", // We assume success if we got past RBAC
			CreatedAt:   time.Now(),
			ClientIP:    r.RemoteAddr, // Handled by service or passed here? Model has ClientIP.
			UserAgent:   r.UserAgent(),
		})
	}()

	// Set Content Type & Disposition
	if req.Format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit_export.csv\"")
	} else {
		w.Header().Set("Content-Type", "application/x-jsonl")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit_export.jsonl\"")
	}

	// Flush headers
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Stream
	err := h.Service.ExportEvents(r.Context(), filter, req.Format, w)
	if err != nil {
		// If headers already sent, we can't send JSON error easily.
		// Log it.
		fmt.Printf("Export stream error: %v\n", err)
	}
}
