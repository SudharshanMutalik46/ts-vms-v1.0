//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func main() {
	// 1. Generate Token
	signingKey := []byte("dev-secret-do-not-use-in-prod")
	tenantID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000001"

	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"tenant_id":  tenantID,
		"sub":        userID,
		"token_type": "access",
		"exp":        now.Add(24 * time.Hour).Unix(),
		"iat":        now.Unix(),
		"nbf":        now.Unix(),
		"jti":        uuid.New().String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "v1"
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		panic(err)
	}

	// 2. Fetch Cameras
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", "http://localhost:8080/api/v1/cameras", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to list cameras: %v. Is server up?\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var cams []map[string]interface{}
	if resp.StatusCode == 200 {
		json.Unmarshal(body, &cams)
	} else {
		fmt.Printf("Error listing cameras: %s %s\n", resp.Status, string(body))
	}

	// 3. Write Output
	output := map[string]interface{}{
		"token":   tokenString,
		"cameras": cams,
	}

	f, _ := os.Create("test_data.json")
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(output)

	fmt.Println("test_data.json created")
}
//go:build ignore


