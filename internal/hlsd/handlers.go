package hlsd

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/platform/paths"
)

var (
	idRegex   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	fileRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+\.(m3u8|mp4|m4s)$`)
)

type Config struct {
	HlsRoot        string
	AllowedOrigins []string
	Keys           KeyProvider
}

type Handler struct {
	cfg   Config
	perms *middleware.PermissionMiddleware
}

func NewHandler(cfg Config, perms *middleware.PermissionMiddleware) *Handler {
	return &Handler{cfg: cfg, perms: perms}
}

func (h *Handler) ServeHLS(w http.ResponseWriter, r *http.Request) {
	// 0. Handle CORS
	h.applyCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	tenantID := chi.URLParam(r, "tenant_id")
	cameraID := chi.URLParam(r, "camera_id")
	sessionID := chi.URLParam(r, "session_id")
	file := chi.URLParam(r, "file")

	// 1. Strict Input Validation
	if !idRegex.MatchString(tenantID) || !idRegex.MatchString(cameraID) || !idRegex.MatchString(sessionID) || !fileRegex.MatchString(file) {
		http.Error(w, "Invalid request parameters", http.StatusBadRequest)
		return
	}

	// 2. Tenant Isolation
	if tenantID == "" {
		http.Error(w, "Missing tenant_id", http.StatusBadRequest)
		return
	}

	// 4. Token Validation (Query or Cookie)
	// Check Query Params FIRST (works for both playlist and segments if propagated)
	err := ValidateHLSToken(cameraID, sessionID, r.URL.Query(), h.cfg.Keys)
	if err == nil {
		// Valid Query Token.
		// For playlists, we inject the cookie for clients that support it (fallback)
		if strings.HasSuffix(file, ".m3u8") {
			tokenCookie := &http.Cookie{
				Name:     fmt.Sprintf("hls_token_%s", sessionID),
				Value:    r.URL.RawQuery,
				Path:     fmt.Sprintf("/hls/live/%s/%s/%s/", tenantID, cameraID, sessionID),
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			}
			http.SetCookie(w, tokenCookie)
		}
	} else {
		// Query param validation failed or missing. Try Cookie.
		cookie, cookieErr := r.Cookie(fmt.Sprintf("hls_token_%s", sessionID))
		if cookieErr != nil {
			http.Error(w, "Unauthorized (Missing Token)", http.StatusUnauthorized)
			return
		}
		// Validate token from cookie value
		q, _ := url.ParseQuery(cookie.Value)
		if err := ValidateHLSToken(cameraID, sessionID, q, h.cfg.Keys); err != nil {
			http.Error(w, "Unauthorized (Invalid Cookie Token)", http.StatusUnauthorized)
			return
		}
	}

	// 3. RBAC Check (Camera View) — only when a JWT auth context is present.
	// HMAC-authenticated requests (GStreamer, HLS players with signed URL) have no
	// JWT context; the signed HMAC already proves the control plane authorized the
	// session, so RBAC is skipped. Only JWT-authenticated callers (browser) are
	// checked via RBAC.
	if _, hasJWT := middleware.GetAuthContext(r.Context()); hasJWT {
		allowed, rbacErr := h.perms.CheckPermission(r.Context(), "camera.view", "camera", cameraID)
		if rbacErr != nil || !allowed {
			http.Error(w, "Forbidden (RBAC)", http.StatusForbidden)
			return
		}
	}

	// 5. Path Resolution — resolve actual segment directory (viewer UUID may differ from
	//    media plane's session directory name; fall back to the most recently modified one).
	resolvedSessionID := h.resolveSessionDir(cameraID, sessionID)

	if strings.HasSuffix(file, ".m3u8") {
		// FIX: generatePlaylist now returns (playlist, isReady, error).
		// When isReady is false (no segments yet), we return a valid empty live
		// playlist with HTTP 200 so GStreamer keeps polling cleanly.
		// The old code returned HTTP 500 on "no segments found", which caused
		// libsoup to log a transport error every 2 s and GStreamer's hlsdemux
		// to stall — it never received a parseable playlist, so it never buffered,
		// never decoded, and ASYNC_DONE never fired.
		pl, isReady, err := h.generatePlaylist(cameraID, resolvedSessionID, r.URL.RawQuery)
		if err != nil {
			log.Printf("[hlsd] Playlist generation error cam=%s sess=%s: %v", cameraID, resolvedSessionID, err)
			http.Error(w, "Playlist generation error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		segCount := strings.Count(pl, "#EXTINF")
		if !isReady {
			// Empty live playlist — GStreamer will re-poll after EXT-X-TARGETDURATION (2 s).
			// This is valid HLS spec behaviour: live streams may have empty playlists while
			// the encoder is starting up. Returning 500 here broke the pipeline completely.
			log.Printf("[hlsd] playlist cam=%s viewSess=%s resolved=%s ready=false segs=0", cameraID, sessionID, resolvedSessionID)
		} else {
			log.Printf("[hlsd] playlist cam=%s viewSess=%s resolved=%s ready=true segs=%d", cameraID, sessionID, resolvedSessionID, segCount)
		}
		w.Write([]byte(pl))
		return
	}

	targetPath, err := paths.SafeJoin(h.cfg.HlsRoot, "live", cameraID, resolvedSessionID, file)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 7. Headers & Serving
	if strings.HasSuffix(file, ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "private, max-age=3600")
	} else if strings.HasSuffix(file, ".m4s") {
		w.Header().Set("Content-Type", "video/iso.segment")
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}

	http.ServeFile(w, r, targetPath)
}

// resolveSessionDir returns the actual HLS segment directory for a camera.
// The viewer session UUID used in the URL may differ from the media plane's
// session directory name. If the exact directory is missing, fall back to
// the most recently modified session directory under the camera folder.
func (h *Handler) resolveSessionDir(cameraID, sessionID string) string {
	dir := filepath.Join(h.cfg.HlsRoot, "live", cameraID, sessionID)
	if _, err := os.Stat(dir); err == nil {
		return sessionID // exact match, use as-is
	}
	cameraDir := filepath.Join(h.cfg.HlsRoot, "live", cameraID)
	entries, err := os.ReadDir(cameraDir)
	if err != nil {
		return sessionID // can't scan, return original (will fail downstream with a clean error)
	}
	var latestName string
	var latestTime int64
	for _, e := range entries {
		if e.IsDir() {
			info, err2 := e.Info()
			if err2 == nil && info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				latestName = e.Name()
			}
		}
	}
	if latestName != "" {
		log.Printf("[hlsd] session dir %q not found for camera %s, using latest: %s", sessionID, cameraID, latestName)
		return latestName
	}
	return sessionID
}

// emptyLivePlaylist is returned when the session directory exists but has no
// segments yet. It is valid HLS — a live stream with zero segments — and
// tells the player to wait and re-poll after EXT-X-TARGETDURATION seconds.
const emptyLivePlaylist = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n"

// generatePlaylist builds an HLS playlist from the segment files on disk.
//
// Returns:
//   - playlist  string — the M3U8 text to send (always valid HLS, never empty)
//   - isReady   bool   — true if at least one segment is available
//   - err       error  — non-nil only for I/O failures (not for "no segments yet")
//
// The caller should write the playlist with HTTP 200 regardless of isReady;
// returning 500 when there are no segments yet causes GStreamer's hlsdemux to
// abort instead of retrying, which prevents the video feed from ever appearing.
func (h *Handler) generatePlaylist(cameraID, sessionID, rawQuery string) (playlist string, isReady bool, err error) {
	dir := filepath.Join(h.cfg.HlsRoot, "live", cameraID, sessionID)

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		// Directory doesn't exist yet — media-plane hasn't created it.
		// Return a valid empty live playlist, not an error.
		// The client will re-poll after EXT-X-TARGETDURATION.
		log.Printf("[hlsd] Session dir not ready cam=%s sess=%s: %v", cameraID, sessionID, readErr)
		return emptyLivePlaylist, false, nil
	}

	var segments []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".mp4") && strings.HasPrefix(e.Name(), "segment_") {
			segments = append(segments, e.Name())
		}
	}

	if len(segments) == 0 {
		// Directory exists but encoder hasn't written the first segment yet.
		// Return a valid empty live playlist so GStreamer keeps polling.
		return emptyLivePlaylist, false, nil
	}

	// Sort segments by name (segment_00001 < segment_00002)
	sort.Strings(segments)

	if len(segments) > 6 {
		segments = segments[len(segments)-6:]
	}

	// Parse sequence number from the first segment filename
	first := segments[0]
	parts := strings.Split(strings.TrimSuffix(first, ".mp4"), "_")
	seq := 0
	if len(parts) > 1 {
		seq, _ = strconv.Atoi(parts[1])
	}

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:2\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", seq))

	for _, seg := range segments {
		sb.WriteString("#EXTINF:2.000,\n")
		// Embed the original HMAC query string so GStreamer (which does not
		// send cookies for segment requests) can authenticate each segment
		// via query params instead of the cookie set on the manifest response.
		if rawQuery != "" {
			sb.WriteString(seg + "?" + rawQuery + "\n")
		} else {
			sb.WriteString(seg + "\n")
		}
	}

	return sb.String(), true, nil
}

func (h *Handler) applyCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}

	allowed := false
	for _, o := range h.cfg.AllowedOrigins {
		if o == "*" || o == origin {
			allowed = true
			break
		}
	}

	if allowed {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Range, Cookie")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetActiveSession returns the latest active HLS session for a camera
func (h *Handler) GetActiveSession(w http.ResponseWriter, r *http.Request) {
	// Allow CORS for file:// and localhost testing
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cameraID := chi.URLParam(r, "camera_id")
	if !idRegex.MatchString(cameraID) {
		http.Error(w, "Invalid camera_id", http.StatusBadRequest)
		return
	}

	// Find latest session directory
	cameraDir := h.cfg.HlsRoot + "/live/" + cameraID
	entries, err := os.ReadDir(cameraDir)
	if err != nil {
		log.Printf("[DEBUG] GetActiveSession: Error reading dir %s: %v\n", cameraDir, err)
		http.Error(w, "No active session (dir read error)", http.StatusNotFound)
		return
	}
	if len(entries) == 0 {
		log.Printf("[DEBUG] GetActiveSession: Directory empty: %s\n", cameraDir)
		http.Error(w, "No active session (empty dir)", http.StatusNotFound)
		return
	}

	log.Printf("[DEBUG] GetActiveSession: Scanning %d entries in %s\n", len(entries), cameraDir)

	// Find most recently modified session
	var latestSession string
	var latestTime int64
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err == nil && info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				latestSession = entry.Name()
			}
		}
	}

	if latestSession == "" {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"session_id":"%s"}`, latestSession)
}

func (h *Handler) Register(r chi.Router) {
	r.HandleFunc("/hls/live/{tenant_id}/{camera_id}/{session_id}/{file}", h.ServeHLS)
}
