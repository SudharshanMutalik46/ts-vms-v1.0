package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/platform/windows"
)

type WindowsHandler struct{}

func NewWindowsHandler() *WindowsHandler {
	return &WindowsHandler{}
}

type WindowsDiscoveryRequest struct {
	Probe bool `json:"probe"`
}

type WindowsDiscoveryResponse struct {
	Result string                   `json:"result"` // "success" or "partial"
	Reason string                   `json:"reason,omitempty"`
	Hosts  []windows.DiscoveredHost `json:"hosts"`
}

// WindowsDiscoveryHandler handles triggered network discovery runs.
func (h *WindowsHandler) WindowsDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req WindowsDiscoveryRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Bounded configuration as per requirements
	cfg := windows.DiscoveryConfig{
		MaxHosts:   1024,
		TimeBudget: 25 * time.Second, // Bounded time budget
		Probe:      req.Probe,
	}

	hosts, err := windows.ScanLAN(r.Context(), cfg)

	resp := WindowsDiscoveryResponse{
		Result: "success",
		Hosts:  hosts,
	}

	if err != nil {
		// Resilience: return partial results with a stable reason on failure
		resp.Result = "partial"
		resp.Reason = err.Error()
		if hosts == nil {
			resp.Hosts = []windows.DiscoveredHost{}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
