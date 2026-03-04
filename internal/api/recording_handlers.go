package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/recording"
)

type RecordingAPI struct {
	DB recording.IMetadataDB
}

// Ensure caller has valid JWT claims.
func checkRBAC(r *http.Request, requiredPermission string) bool {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		return false
	}
	return ac.HasPermission(requiredPermission) || ac.HasRole("admin")
}

func (api *RecordingAPI) HandleGetSegments(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.view") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	camID := r.PathValue("id")
	if camID == "" {
		camID = r.URL.Query().Get("camera_id")
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, _ := time.Parse(time.RFC3339, fromStr)
	to, _ := time.Parse(time.RFC3339, toStr)

	// If no time range provided, default to last 24h
	if from.IsZero() {
		from = time.Now().Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = time.Now()
	}

	segments, err := api.DB.GetSegments(r.Context(), camID, from, to)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(segments)
}

func (api *RecordingAPI) HandleCreateEvent(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var ev recording.Event
	json.NewDecoder(r.Body).Decode(&ev)
	ev.EventTS = time.Now()

	api.DB.CreateEvent(r.Context(), &ev)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ev)
}

func (api *RecordingAPI) HandleLinkSegment(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	eventID := r.URL.Query().Get("event_id")
	segmentID := r.URL.Query().Get("segment_id")

	err := api.DB.LinkSegmentToEvent(r.Context(), eventID, segmentID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}
