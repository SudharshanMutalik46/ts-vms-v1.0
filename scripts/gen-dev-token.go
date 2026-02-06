//go:build ignore

package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func main() {
	signingKey := []byte("dev-secret-do-not-use-in-prod")
	tenantID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000001" // Existing admin user

	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"tenant_id":  tenantID,
		"sub":        userID,
		"token_type": "access", // match tokens.Access
		"exp":        now.Add(24 * time.Hour).Unix(),
		"iat":        now.Unix(),
		"nbf":        now.Unix(),
		"jti":        uuid.New().String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "v1"

	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\n--- Dev Access Token ---\n")
	fmt.Printf("Token: %s\n", tokenString)

	// Verify
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", "http://localhost:8080/api/v1/cameras", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Request Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("Response Status: %s\n", resp.Status)
	if resp.StatusCode == 200 {
		fmt.Println("SUCCESS: Auth works!")
	} else {
		fmt.Println("FAILURE: Auth failed.")
		// Print body
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		fmt.Printf("Body: %s\n", string(buf[:n]))
	}
}
//go:build ignore


