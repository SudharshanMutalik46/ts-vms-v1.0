package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"sub"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

func main() {
	key := []byte("dev-secret-do-not-use-in-prod")

	userID := "00000000-0000-0000-0000-000000000002"
	tenantID := "00000000-0000-0000-0000-000000000001"

	now := time.Now().UTC()
	claims := Claims{
		TenantID:  tenantID,
		UserID:    userID,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)), // 24 Hours!
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "v1"

	tokenString, err := token.SignedString(key)
	if err != nil {
		panic(err)
	}

	fmt.Println("Generated 24h Token:")
	fmt.Println(tokenString)
	os.WriteFile("token.txt", []byte(tokenString), 0644)
}
