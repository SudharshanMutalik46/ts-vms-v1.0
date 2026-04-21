package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/recording"
)

type RecordingAPI struct {
	DB                recording.IMetadataDB
	SegmentRefresher  func(ctx context.Context, cameraID string) error
	SegmentDiskLoader func(ctx context.Context, cameraID string, from, to time.Time) ([]recording.ArchiveSegment, error)
}

// Ensure caller has valid JWT claims.
func checkRBAC(r *http.Request, requiredPermission string) bool {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		return false
	}
	return ac.HasPermission(requiredPermission) || ac.HasRole("admin")
}

func checkAnyRBAC(r *http.Request, requiredPermissions ...string) bool {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		return false
	}
	if ac.HasRole("admin") {
		return true
	}
	for _, perm := range requiredPermissions {
		if ac.HasPermission(perm) {
			return true
		}
	}
	return false
}

func (api *RecordingAPI) HandleGetSegments(w http.ResponseWriter, r *http.Request) {
	if !checkAnyRBAC(r, "recording.view", "video.view") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	started := time.Now()

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

	if camID == "" {
		http.Error(w, "camera_id is required", http.StatusBadRequest)
		return
	}
	log.Printf("[recording.segments] start camera_id=%s from=%s to=%s", camID, from.Format(time.RFC3339), to.Format(time.RFC3339))

	segments, err := api.DB.GetSegments(r.Context(), camID, from, to)
	if err != nil {
		log.Printf("[recording.segments] db_error camera_id=%s err=%v elapsed=%s", camID, err, time.Since(started))
		http.Error(w, err.Error(), 500)
		return
	}
	log.Printf("[recording.segments] db_result camera_id=%s count=%d elapsed=%s", camID, len(segments), time.Since(started))

	if len(segments) == 0 && api.SegmentRefresher != nil {
		refreshStarted := time.Now()
		refreshCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		if err := api.SegmentRefresher(refreshCtx, camID); err != nil {
			log.Printf("[WARNING] recording.segment.refresh failed camera_id=%s: %v", camID, err)
		} else if refreshed, refreshErr := api.DB.GetSegments(r.Context(), camID, from, to); refreshErr == nil {
			segments = refreshed
			log.Printf("[recording.segments] refresh_result camera_id=%s count=%d elapsed=%s", camID, len(segments), time.Since(refreshStarted))
		}
	}

	if len(segments) == 0 && api.SegmentDiskLoader != nil {
		diskStarted := time.Now()
		if diskSegments, err := api.SegmentDiskLoader(r.Context(), camID, from, to); err != nil {
			log.Printf("[WARNING] recording.segment.disk_load failed camera_id=%s: %v", camID, err)
		} else {
			segments = diskSegments
			log.Printf("[recording.segments] disk_result camera_id=%s count=%d elapsed=%s", camID, len(segments), time.Since(diskStarted))
		}
	}

	log.Printf("[recording.segments] done camera_id=%s final_count=%d total_elapsed=%s", camID, len(segments), time.Since(started))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(segments)
}

func (api *RecordingAPI) HandleGetRecordedCameras(w http.ResponseWriter, r *http.Request) {
	if !checkAnyRBAC(r, "recording.view", "video.view") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok || ac == nil || ac.TenantID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	from, _ := time.Parse(time.RFC3339, fromStr)
	to, _ := time.Parse(time.RFC3339, toStr)

	if from.IsZero() {
		from = time.Now().Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = time.Now()
	}

	log.Printf("[recording.cameras_with_recordings] tenant=%s from=%s to=%s", ac.TenantID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	cameras, err := api.DB.GetRecordedCameras(r.Context(), ac.TenantID, from, to)
	if err != nil {
		log.Printf("[recording.cameras_with_recordings] error tenant=%s err=%v", ac.TenantID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[recording.cameras_with_recordings] count=%d tenant=%s", len(cameras), ac.TenantID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cameras)
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
