package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// Config
var (
	baseURL           string
	serviceToken      string
	natsURL           string
	maxOverlayCameras int
	weaponEnabled     bool

	// Metrics (atomic counters)
	basicInferenceTotal  int64
	weaponInferenceTotal int64
	framesDroppedTotal   int64
	serviceUp            int64 = 1
)

// 10-class label set for basic detection
var basicLabels = []string{
	"person", "car", "truck", "bus", "motorcycle", "bicycle", "cat", "dog", "bird", "bag",
}

// Weapon labels (feature-flagged)
var weaponLabels = []string{"handgun", "rifle", "knife"}

func main() {
	baseURL = getEnv("API_BASE_URL", "http://localhost:8080")
	serviceToken = getEnv("AI_SERVICE_TOKEN", "dev_ai_secret")
	natsURL = getEnv("NATS_URL", "nats://localhost:4222")
	maxOverlayCameras = getEnvInt("MAX_OVERLAY_CAMERAS", 8)
	weaponEnabled = getEnv("WEAPON_AI_ENABLED", "false") == "true"

	log.Printf("[AI Service] Starting - API: %s, NATS: %s, MaxCameras: %d, WeaponEnabled: %t",
		baseURL, natsURL, maxOverlayCameras, weaponEnabled)

	// Initialize ONNX detector
	exePath, _ := os.Executable()
	modelDir := filepath.Join(filepath.Dir(exePath), "models")
	if _, err := os.Stat(modelDir); os.IsNotExist(err) {
		modelDir = filepath.Join(".", "models") // Development fallback
	}
	if err := InitDetector(modelDir); err != nil {
		log.Printf("[AI Service] Detector init failed: %v (using mock)", err)
	}
	defer CleanupDetector()

	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Printf("[AI Service] NATS connection failed: %v (will use HTTP fallback)", err)
		nc = nil
	} else {
		defer nc.Close()
		log.Printf("[AI Service] NATS connected")
	}

	// Start Health Endpoint
	go startHealthServer()

	client := &http.Client{Timeout: 5 * time.Second}

	// Timing state for weapon (0.25 FPS = 4s interval)
	lastWeaponRun := time.Now().Add(-4 * time.Second)

	for {
		loopStart := time.Now()

		if err := runLoop(client, nc, &lastWeaponRun); err != nil {
			log.Printf("[AI Service] Loop error: %v", err)
		}

		// Throttle ~ 2s total interval (0.5 FPS for basic)
		elapsed := time.Since(loopStart)
		if elapsed < 2*time.Second {
			time.Sleep(2*time.Second - elapsed)
		}
	}
}

func startHealthServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                 "ok",
			"basic_inference_total":  atomic.LoadInt64(&basicInferenceTotal),
			"weapon_inference_total": atomic.LoadInt64(&weaponInferenceTotal),
			"frames_dropped_total":   atomic.LoadInt64(&framesDroppedTotal),
			"service_up":             atomic.LoadInt64(&serviceUp),
		})
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# HELP ai_inference_total Total inference runs\n")
		fmt.Fprintf(w, "# TYPE ai_inference_total counter\n")
		fmt.Fprintf(w, "ai_inference_total{stream=\"basic\"} %d\n", atomic.LoadInt64(&basicInferenceTotal))
		fmt.Fprintf(w, "ai_inference_total{stream=\"weapon\"} %d\n", atomic.LoadInt64(&weaponInferenceTotal))
		fmt.Fprintf(w, "# HELP ai_frames_dropped_total Frames dropped due to overload\n")
		fmt.Fprintf(w, "# TYPE ai_frames_dropped_total counter\n")
		fmt.Fprintf(w, "ai_frames_dropped_total{stream=\"basic\"} %d\n", atomic.LoadInt64(&framesDroppedTotal))
		fmt.Fprintf(w, "# HELP ai_service_up Service health\n")
		fmt.Fprintf(w, "# TYPE ai_service_up gauge\n")
		fmt.Fprintf(w, "ai_service_up 1\n")
	})

	log.Printf("[AI Service] Health server starting on :8090")
	if err := http.ListenAndServe(":8090", nil); err != nil {
		log.Printf("[AI Service] Health server failed: %v", err)
	}
}

func runLoop(client *http.Client, nc *nats.Conn, lastWeaponRun *time.Time) error {
	// 1. Get Active Cameras
	cams, err := getActiveCameras(client)
	if err != nil {
		return err
	}
	if len(cams) == 0 {
		return nil
	}

	// 2. Bounded sampling: limit to maxOverlayCameras
	if len(cams) > maxOverlayCameras {
		atomic.AddInt64(&framesDroppedTotal, int64(len(cams)-maxOverlayCameras))
		cams = cams[:maxOverlayCameras]
	}

	// 3. Determine if weapon run is due (0.25 FPS = every 4s)
	runWeapon := weaponEnabled && time.Since(*lastWeaponRun) >= 4*time.Second
	if runWeapon {
		*lastWeaponRun = time.Now()
	}

	// 4. Process Each Camera
	for _, c := range cams {
		processCamera(client, nc, c.CameraID, runWeapon)
	}

	return nil
}

type ActiveCam struct {
	CameraID string `json:"camera_id"`
	TenantID string `json:"tenant_id"`
}

func getActiveCameras(client *http.Client) ([]ActiveCam, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/internal/cameras/active", nil)
	req.Header.Set("Authorization", "Bearer "+serviceToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("active cameras error: %d", resp.StatusCode)
	}

	var list []ActiveCam
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func processCamera(client *http.Client, nc *nats.Conn, camID string, runWeapon bool) {
	// A. Fetch Snapshot
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/internal/cameras/%s/snapshot", baseURL, camID), nil)
	req.Header.Set("Authorization", "Bearer "+serviceToken)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[%s] Snapshot fetch failed: %v", camID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return
	}

	// Read snapshot JPEG data for real detection
	jpegData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] Snapshot read failed: %v", camID, err)
		return
	}

	// B. Run Basic Detection (real or mock fallback)
	basicObjects := RunDetection(jpegData, "basic")
	if basicObjects == nil {
		basicObjects = []Object{} // Empty detection is valid
	}

	basicPayload := DetectionPayload{
		CameraID: camID,
		TSUnixMS: time.Now().UnixMilli(),
		Stream:   "basic",
		Objects:  basicObjects,
	}
	publishDetection(nc, "detections.basic."+camID, basicPayload)
	atomic.AddInt64(&basicInferenceTotal, 1)

	// C. Run Weapon Detection (if enabled and due)
	if runWeapon {
		weaponObjects := RunDetection(jpegData, "weapon")
		if weaponObjects != nil && len(weaponObjects) > 0 {
			weaponPayload := DetectionPayload{
				CameraID: camID,
				TSUnixMS: time.Now().UnixMilli(),
				Stream:   "weapon",
				Objects:  weaponObjects,
			}
			publishDetection(nc, "detections.weapon."+camID, weaponPayload)
		}
		atomic.AddInt64(&weaponInferenceTotal, 1)
	}
}

type DetectionPayload struct {
	CameraID string   `json:"camera_id"`
	TSUnixMS int64    `json:"ts_unix_ms"`
	Stream   string   `json:"stream"`
	Objects  []Object `json:"objects"`
}

type Object struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	BBox       BBox    `json:"bbox"`
}

type BBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func mockBasicObjects() []Object {
	count := rand.Intn(3) + 1 // 1-3 objects
	objs := make([]Object, count)

	for i := 0; i < count; i++ {
		x := rand.Float64() * 0.7
		y := rand.Float64() * 0.7
		w := 0.1 + rand.Float64()*0.2
		h := 0.1 + rand.Float64()*0.2

		// Ensure x+w <= 1 and y+h <= 1
		if x+w > 1 {
			w = 1 - x
		}
		if y+h > 1 {
			h = 1 - y
		}

		objs[i] = Object{
			Label:      basicLabels[rand.Intn(len(basicLabels))],
			Confidence: 0.6 + rand.Float64()*0.4, // 0.6 - 1.0
			BBox:       BBox{X: x, Y: y, W: w, H: h},
		}
	}
	return objs
}

func mockWeaponObjects() []Object {
	x := rand.Float64() * 0.7
	y := rand.Float64() * 0.7
	w := 0.05 + rand.Float64()*0.1
	h := 0.05 + rand.Float64()*0.1

	if x+w > 1 {
		w = 1 - x
	}
	if y+h > 1 {
		h = 1 - y
	}

	return []Object{
		{
			Label:      weaponLabels[rand.Intn(len(weaponLabels))],
			Confidence: 0.7 + rand.Float64()*0.3,
			BBox:       BBox{X: x, Y: y, W: w, H: h},
		},
	}
}

func publishDetection(nc *nats.Conn, subject string, payload DetectionPayload) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Marshal error: %v", err)
		return
	}

	if nc != nil {
		if err := nc.Publish(subject, data); err != nil {
			log.Printf("NATS publish failed: %v", err)
		}
	} else {
		// Fallback: log only (HTTP ingest is dev-only and disabled by default)
		log.Printf("[NATS-MOCK] %s: %s", subject, string(data))
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
