package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Config
const (
	BaseURL    = "http://localhost:8082/api/v1"
	SigningKey = "dev-secret-do-not-use-in-prod" // Same as vms-control
	TenantID   = "00000000-0000-0000-0000-000000000001"
	SiteID     = "00000000-0000-0000-0000-000000000001"
	UserID     = "00000000-0000-0000-0000-000000000001"
)

func main() {
	// 1. Generate Token
	token := generateToken()
	fmt.Println("Token Generated.")

	// 2. Use Seeded Camera
	camID := "00000000-0000-0000-0000-000000000001"
	fmt.Printf("Using Seeded Camera: %s\n", camID)

	// 3. Join Room
	joinRoom(token, camID)
}

func generateToken() string {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"tenant_id":  TenantID,
		"sub":        UserID,
		"token_type": "access",
		"exp":        now.Add(1 * time.Hour).Unix(),
		"iat":        now.Unix(),
		"nbf":        now.Unix(),
		"jti":        uuid.New().String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "v1"
	s, _ := token.SignedString([]byte(SigningKey))
	return s
}

func addCamera(token string) string {
	// Create request
	body := map[string]interface{}{
		"name":     "SFU Test Camera " + uuid.New().String(),
		"rtsp_url": "rtsp://192.168.1.1:8554/live",
		"site_id":  SiteID,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", BaseURL+"/cameras", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("Add Camera Failed: %d %s", resp.StatusCode, string(b)))
	}

	var res struct {
		ID string `json:"id"`
	}
	// Parse response (might be wrapped or direct)
	// API usually returns { "id": "...", ... }
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		panic(err)
	}
	return res.ID
}

func joinRoom(token, camID string) {
	sessionID := uuid.New().String()
	body := map[string]interface{}{
		"sessionId": sessionID,
	}
	jsonBody, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/sfu/rooms/%s/join", BaseURL, camID)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("Joining Room: %s (Session: %s)\n", camID, sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Printf("Join FAILED: %d\nBody: %s\n", resp.StatusCode, string(b))
	} else {
		fmt.Printf("Join SUCCESS!\nResponse Length: %d\n", len(b))
		// Optional: Print slice of body or RTP Caps
		if len(b) > 200 {
			fmt.Printf("Body (Partial): %s...\n", string(b[:200]))
		} else {
			fmt.Printf("Body: %s\n", string(b))
		}
	}
}
