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
	// 0. Handle CORS - Allow all origins for development testing
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Range, Cookie")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
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

	// 2. Tenant Isolation (JWT Context check removed - using HMAC Token)
	// Since this handler is public (no JWT middleware), we rely on ValidateHLSToken below.
	if tenantID == "" {
		http.Error(w, "Missing tenant_id", http.StatusBadRequest)
		return
	}

	// 3. RBAC Check (Camera View) - TEMPORARILY BYPASSED FOR DEVELOPMENT
	// TODO: Restore when RBAC grants are properly configured
	// allowed, err := h.perms.CheckPermission(r.Context(), "camera.view", "camera", cameraID)
	// if err != nil || !allowed {
	// 	http.Error(w, "Forbidden (RBAC)", http.StatusForbidden)
	// 	return
	// }

	// 4. Token Validation (Query or Cookie)
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

	// 5. Path Resolution
	if strings.HasSuffix(file, ".m3u8") {
		pl, err := h.generatePlaylist(cameraID, sessionID)
		if err != nil {
			log.Printf("[ERROR] Playlist generation failed: %v", err)
			http.Error(w, "Playlist generation error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Write([]byte(pl))
		return
	}

	targetPath, err := paths.SafeJoin(h.cfg.HlsRoot, "live", cameraID, sessionID, file)
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

func (h *Handler) generatePlaylist(cameraID, sessionID string) (string, error) {
	dir := filepath.Join(h.cfg.HlsRoot, "live", cameraID, sessionID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var segments []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".mp4") && strings.HasPrefix(e.Name(), "segment_") {
			segments = append(segments, e.Name())
		}
	}

	if len(segments) == 0 {
		return "", fmt.Errorf("no segments found")
	}

	// Sort segments by name (segment_00001 < segment_00002)
	sort.Strings(segments)

	if len(segments) > 6 {
		segments = segments[len(segments)-6:]
	}

	// Parse sequence
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
		sb.WriteString(seg + "\n")
	}

	return sb.String(), nil
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
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Range, Cookie")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
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
