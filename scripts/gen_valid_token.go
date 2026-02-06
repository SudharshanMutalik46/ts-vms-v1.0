//go:build ignore

package main

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TokenType string

const Access TokenType = "access"

type Claims struct {
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"sub"`
	TokenType TokenType `json:"token_type"`
	jwt.RegisteredClaims
}

func main() {
	signingKey := []byte("dev-secret-do-not-use-in-prod")
	tenantID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000001"

	now := time.Now().UTC()
	claims := Claims{
		TenantID:  tenantID,
		UserID:    userID,
		TokenType: Access,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "v1"
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		panic(err)
	}

	fmt.Println(tokenString)
}
//go:build ignore


