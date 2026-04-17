package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/recording"
)

type recordingSegmentsMockDB struct {
	segments  []recording.ArchiveSegment
	refreshed bool
}

func (m *recordingSegmentsMockDB) Available() bool { return true }

func (m *recordingSegmentsMockDB) GetSegments(ctx context.Context, cameraID string, from, to time.Time) ([]recording.ArchiveSegment, error) {
	if !m.refreshed {
		return nil, nil
	}
	out := make([]recording.ArchiveSegment, len(m.segments))
	copy(out, m.segments)
	return out, nil
}

func (m *recordingSegmentsMockDB) GetLatestSegmentEnd(ctx context.Context, cameraID string) (time.Time, error) {
	return time.Time{}, nil
}

func (m *recordingSegmentsMockDB) UpsertFinalizedSegment(ctx context.Context, seg *recording.ArchiveSegment) error {
	return nil
}

func (m *recordingSegmentsMockDB) MarkMissing(ctx context.Context, path string) error { return nil }
func (m *recordingSegmentsMockDB) MarkCorrupt(ctx context.Context, path, quarantinePath string) error {
	return nil
}
func (m *recordingSegmentsMockDB) CreateEvent(ctx context.Context, ev *recording.Event) error {
	return nil
}
func (m *recordingSegmentsMockDB) LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error {
	return nil
}
func (m *recordingSegmentsMockDB) GetCredentials(ctx context.Context, cameraID string) (*data.CameraCredential, error) {
	return nil, nil
}
func (m *recordingSegmentsMockDB) DecryptCredentials(cred *data.CameraCredential, keyring *crypto.Keyring) (string, string, error) {
	return "", "", nil
}
func (m *recordingSegmentsMockDB) AuditRecoveryEvent(ctx context.Context, path, state, detail string) error {
	return nil
}
func (m *recordingSegmentsMockDB) ExpectedPathsSince(ctx context.Context, since time.Time) ([]string, error) {
	return nil, nil
}
func (m *recordingSegmentsMockDB) CreateExportJob(ctx context.Context, job *recording.ExportJob, requestedBy string) error {
	return nil
}
func (m *recordingSegmentsMockDB) GetExportJob(ctx context.Context, id string) (*recording.ExportJob, error) {
	return nil, nil
}
func (m *recordingSegmentsMockDB) UpdateExportJob(ctx context.Context, job *recording.ExportJob) error {
	return nil
}

func TestHandleGetSegmentsReconcilesDiskWhenIndexIsEmpty(t *testing.T) {
	mockDB := &recordingSegmentsMockDB{
		segments: []recording.ArchiveSegment{
			{
				ID:         "seg-1",
				CameraID:   "cam-1",
				StartTS:    time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC),
				EndTS:      time.Date(2026, 4, 4, 20, 1, 0, 0, time.UTC),
				DurationMs: 60000,
				Path:       `C:\ts_vms_storage\tenant_sys\site_hq\cam-1\20260404_200000\segment_00000.mkv`,
				FilePath:   `C:\ts_vms_storage\tenant_sys\site_hq\cam-1\20260404_200000\segment_00000.mkv`,
				FileSize:   1234,
				SizeBytes:  1234,
				Container:  "mkv",
				VideoCodec: "H265",
				Finalized:  true,
			},
		},
	}

	api := &RecordingAPI{
		DB: mockDB,
		SegmentRefresher: func(ctx context.Context, cameraID string) error {
			mockDB.refreshed = true
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/recording/cameras/cam-1/segments?camera_id=cam-1&from=2026-04-04T00:00:00Z&to=2026-04-05T00:00:00Z", nil)
	req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		UserID: "user-1",
		Roles:  []string{"admin"},
	}))
	w := httptest.NewRecorder()

	api.HandleGetSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got []recording.ArchiveSegment
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment after refresh, got %d", len(got))
	}
	if !mockDB.refreshed {
		t.Fatalf("expected segment refresher to be called")
	}
	if got[0].VideoCodec != "H265" {
		t.Fatalf("expected H265 video_codec, got %+v", got[0])
	}
}

func TestHandleGetSegmentsFallsBackToDiskLoader(t *testing.T) {
	mockDB := &recordingSegmentsMockDB{}

	api := &RecordingAPI{
		DB: mockDB,
		SegmentRefresher: func(ctx context.Context, cameraID string) error {
			return nil
		},
		SegmentDiskLoader: func(ctx context.Context, cameraID string, from, to time.Time) ([]recording.ArchiveSegment, error) {
			return []recording.ArchiveSegment{
				{
					ID:         "seg-disk-1",
					CameraID:   cameraID,
					StartTS:    from.Add(10 * time.Minute),
					EndTS:      from.Add(11 * time.Minute),
					DurationMs: 60000,
					Path:       `C:\ts_vms_storage\tenant_sys\site_hq\cam-1\20260404_200000\segment_00000.mkv`,
					FilePath:   `C:\ts_vms_storage\tenant_sys\site_hq\cam-1\20260404_200000\segment_00000.mkv`,
					FileSize:   1234,
					SizeBytes:  1234,
					Container:  "mkv",
					VideoCodec: "H264",
					Finalized:  true,
				},
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/recording/cameras/cam-1/segments?camera_id=cam-1&from=2026-04-04T00:00:00Z&to=2026-04-05T00:00:00Z", nil)
	req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		UserID: "user-1",
		Roles:  []string{"admin"},
	}))
	w := httptest.NewRecorder()

	api.HandleGetSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got []recording.ArchiveSegment
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment from disk loader, got %d", len(got))
	}
	if got[0].ID != "seg-disk-1" {
		t.Fatalf("unexpected segment returned: %+v", got[0])
	}
	if got[0].VideoCodec != "H264" {
		t.Fatalf("expected H264 video_codec, got %+v", got[0])
	}
}
