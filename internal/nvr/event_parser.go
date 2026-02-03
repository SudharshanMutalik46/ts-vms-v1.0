package nvr

import (
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

// ConvertAdapterEvent converts generic adapter event to VmsEvent
// Parse logic happens in Adapter (Phase 2.7) or here?
// Requirement: "Parse manufacturer... in Control Plane".
// Since `adapters.NvrEvent` has `RawVendorType` and `RawPayload`,
// we map it here.
func ConvertAdapterEvent(nvrID, tenantID, siteID uuid.UUID, ae adapters.NvrEvent, vendor string) (*VmsEvent, error) {
	// Map Type/Severity
	vType, vSev := MapVendorEvent(vendor, ae.RawVendorType)

	evt := &VmsEvent{
		EventID:    uuid.New(),
		Source:     "nvr",
		Vendor:     vendor,
		TenantID:   tenantID,
		SiteID:     siteID,
		NVRID:      nvrID,
		ChannelRef: ae.ChannelRef,
		EventType:  vType,
		Severity:   vSev,
		OccurredAt: ae.OccurredAt,
		ReceivedAt: time.Now(),    // Approximate or actual
		Raw:        ae.RawPayload, // Already redacted by adapter
	}

	// Snapshot Hook (Option A: Vendor Ref)
	// If adapter extracted snapshot URL/Ref into RawPayload or struct
	// Let's check ae.RawPayload for "snapshot_url" or "picture"
	if val, ok := ae.RawPayload["snapshot_url"]; ok {
		if str, ok := val.(string); ok {
			evt.Snapshot.VendorRef = str
		}
	}

	return evt, nil
}
