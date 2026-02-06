package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

type ActiveCamera struct {
	CameraID string `json:"camera_id"`
	TenantID string `json:"tenant_id"`
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

func main() {
	cpURL := "http://localhost:8080"
	natsURL := nats.DefaultURL
	token := "dev_ai_secret"

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("NATS connect error: %v", err)
	}
	defer nc.Close()

	log.Printf("Mock AI Bridge started. CP=%s NATS=%s", cpURL, natsURL)

	ticker := time.NewTicker(time.Second)
	boxX := 0.05
	boxDir := 0.01

	for range ticker.C {
		// 1. Get Active Cameras
		req, _ := http.NewRequest("GET", cpURL+"/api/v1/internal/cameras/active", nil)
		req.Header.Set("X-AI-Service-Token", token)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error fetching active cameras: %v", err)
			continue
		}

		var active []ActiveCamera
		json.NewDecoder(resp.Body).Decode(&active)
		resp.Body.Close()

		if len(active) == 0 {
			log.Println("No active demand. Polling...")
			continue
		}

		// Move box (Targeting the person on the left)
		boxX += boxDir
		if boxX > 0.12 || boxX < 0.03 {
			boxDir = -boxDir
		}

		for _, cam := range active {
			log.Printf("Processing demand for camera: %s", cam.CameraID)

			// 2. Fetch Snapshot (Verify Auth)
			sReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/internal/cameras/%s/snapshot", cpURL, cam.CameraID), nil)
			sReq.Header.Set("X-AI-Service-Token", token)
			sResp, err := client.Do(sReq)
			if err == nil {
				sResp.Body.Close()
				if sResp.StatusCode == 200 {
					log.Printf("Snapshot fetched successfully for %s", cam.CameraID)
				} else {
					log.Printf("Snapshot fetch failed for %s: %d", cam.CameraID, sResp.StatusCode)
				}
			}

			// 3. Publish Mock Detection
			payload := DetectionPayload{
				CameraID: cam.CameraID,
				TSUnixMS: time.Now().UnixMilli(),
				Stream:   "basic",
				Objects: []Object{
					{
						Label:      "person",
						Confidence: 0.98,
						BBox: BBox{
							X: boxX,
							Y: 0.2, // Head/Torso area
							W: 0.25,
							H: 0.6,
						},
					},
				},
			}

			data, _ := json.Marshal(payload)
			subject := fmt.Sprintf("detections.basic.%s", cam.CameraID)
			nc.Publish(subject, data)
			log.Printf("Published mock detection to %s", subject)
		}
	}
}
