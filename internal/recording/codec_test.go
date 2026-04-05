package recording

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInferCodecFromRTSPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "h264 suffix", url: "rtsp://10.0.0.1/live/ch01.264", want: "h264"},
		{name: "h264 token", url: "rtsp://10.0.0.1/live/h264/main", want: "h264"},
		{name: "h265 token", url: "rtsp://10.0.0.1/live/hevc/main", want: "h265"},
		{name: "empty", url: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferCodecFromRTSPURL(tc.url); got != tc.want {
				t.Fatalf("inferCodecFromRTSPURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestExtractRTSPHost(t *testing.T) {
	got := extractRTSPHost("rtsp://user:pass@192.168.1.127:554/channel=1_stream=1.sdp")
	if got != "192.168.1.127:554" {
		t.Fatalf("extractRTSPHost() = %q, want 192.168.1.127:554", got)
	}
}

func TestLoadCameraRecordingSources(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectQuery("SELECT\\s+COALESCE\\(s\\.main_profile_token, ''\\),\\s+COALESCE\\(s\\.sub_profile_token, ''\\),\\s+COALESCE\\(s\\.main_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(s\\.sub_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(mp\\.video_codec, ''\\),\\s+COALESCE\\(sp\\.video_codec, ''\\)").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"main_token", "sub_token", "main_rtsp", "sub_rtsp", "main_codec", "sub_codec"}).AddRow("main-profile", "sub-profile", "rtsp://10.0.0.1/main", "rtsp://10.0.0.1/sub", "H264", ""))

	sources, err := store.LoadCameraRecordingSources(context.Background(), "cam-1", "rtsp://10.0.0.1/live/ch01.265")
	if err != nil {
		t.Fatalf("LoadCameraRecordingSources returned error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("LoadCameraRecordingSources len = %d, want 2", len(sources))
	}
	if sources[0].Codec != "h264" || sources[0].RTSPURL != "rtsp://10.0.0.1/main" || sources[0].ProfileToken != "main-profile" {
		t.Fatalf("LoadCameraRecordingSources[0] = %+v, want main h264", sources[0])
	}
	if sources[1].RTSPURL != "rtsp://10.0.0.1/sub" || sources[1].ProfileToken != "sub-profile" {
		t.Fatalf("LoadCameraRecordingSources[1] = %+v, want sub stream", sources[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

func TestLoadCameraRecordingSources_FallsBackToMediaProfileCodec(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectQuery("SELECT\\s+COALESCE\\(s\\.main_profile_token, ''\\),\\s+COALESCE\\(s\\.sub_profile_token, ''\\),\\s+COALESCE\\(s\\.main_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(s\\.sub_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(mp\\.video_codec, ''\\),\\s+COALESCE\\(sp\\.video_codec, ''\\)").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"main_token", "sub_token", "main_rtsp", "sub_rtsp", "main_codec", "sub_codec"}).
			AddRow("", "", "rtsp://10.0.0.1/main.264", "rtsp://10.0.0.1/sub.264", "", ""))

	mock.ExpectQuery("SELECT\\s+COALESCE\\(video_codec, ''\\)\\s+FROM camera_media_profiles").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"video_codec"}).AddRow("H265"))

	sources, err := store.LoadCameraRecordingSources(context.Background(), "cam-1", "rtsp://10.0.0.1/main.264")
	if err != nil {
		t.Fatalf("LoadCameraRecordingSources returned error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("LoadCameraRecordingSources len = %d, want 2", len(sources))
	}
	if sources[0].Codec != "h265" {
		t.Fatalf("LoadCameraRecordingSources[0].Codec = %q, want h265", sources[0].Codec)
	}
	if sources[1].Codec != "h265" {
		t.Fatalf("LoadCameraRecordingSources[1].Codec = %q, want h265", sources[1].Codec)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

func TestLoadEnabledCameras_UsesPreferredRecordingCodec(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectQuery("SELECT\\s+id::text,\\s+COALESCE\\(rtsp_url, ''\\),\\s+COALESCE\\(ip_address::text, ''\\),\\s+COALESCE\\(port, 0\\)").
		WillReturnRows(sqlmock.NewRows([]string{"id", "rtsp_url", "ip", "port"}).
			AddRow("cam-1", "rtsp://10.0.0.1/main.264", "10.0.0.1", 554))
	mock.ExpectQuery("SELECT\\s+EXISTS\\s*\\(").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT\\s+COALESCE\\(preferred_recording_codec, ''\\)").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"preferred_recording_codec"}).AddRow("H265"))
	mock.ExpectQuery("SELECT\\s+COALESCE\\(s\\.main_profile_token, ''\\),\\s+COALESCE\\(s\\.sub_profile_token, ''\\),\\s+COALESCE\\(s\\.main_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(s\\.sub_rtsp_url_sanitized, ''\\),\\s+COALESCE\\(mp\\.video_codec, ''\\),\\s+COALESCE\\(sp\\.video_codec, ''\\)").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"main_token", "sub_token", "main_rtsp", "sub_rtsp", "main_codec", "sub_codec"}).
			AddRow("", "", "", "", "", ""))
	mock.ExpectQuery("SELECT\\s+COALESCE\\(video_codec, ''\\)\\s+FROM camera_media_profiles").
		WithArgs("cam-1").
		WillReturnRows(sqlmock.NewRows([]string{"video_codec"}).AddRow("H265"))

	cams, err := store.LoadEnabledCameras(context.Background())
	if err != nil {
		t.Fatalf("LoadEnabledCameras returned error: %v", err)
	}
	if len(cams) != 1 {
		t.Fatalf("LoadEnabledCameras len = %d, want 1", len(cams))
	}
	if cams[0].PreferredRecordingCodec != "h265" {
		t.Fatalf("PreferredRecordingCodec = %q, want h265", cams[0].PreferredRecordingCodec)
	}
	if cams[0].Codec != "h265" {
		t.Fatalf("Codec = %q, want h265", cams[0].Codec)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

func TestUpdatePreferredRecordingCodec(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectQuery("SELECT\\s+EXISTS\\s*\\(").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("UPDATE\\s+cameras\\s+SET\\s+preferred_recording_codec = NULLIF\\(\\$2, ''\\),\\s+updated_at = NOW\\(\\)\\s+WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs("cam-1", "h265").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdatePreferredRecordingCodec(context.Background(), "cam-1", "H265"); err != nil {
		t.Fatalf("UpdatePreferredRecordingCodec returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}
