package tokens

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("invalid token")

type TokenType string

const (
	Access  TokenType = "access"
	Refresh TokenType = "refresh"
)

type Claims struct {
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"sub"`
	TokenType TokenType `json:"token_type"`
	jwt.RegisteredClaims
}

type Manager struct {
	signingKey []byte
}

func NewManager(signingKey string) *Manager {
	return &Manager{signingKey: []byte(signingKey)}
}

func (m *Manager) GenerateAccessToken(userID, tenantID string) (string, error) {
	return m.generateToken(userID, tenantID, Access, 15*time.Minute)
}

func (m *Manager) GenerateRefreshToken(userID, tenantID string) (string, error) {
	return m.generateToken(userID, tenantID, Refresh, 7*24*time.Hour)
}

func (m *Manager) generateToken(userID, tenantID string, tokenType TokenType, duration time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		TenantID:  tenantID,
		UserID:    userID,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(), // jti
			Subject:   userID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Add Kid for future key rotation support, even if using single key now
	token.Header["kid"] = "v1"

	return token.SignedString(m.signingKey)
}

func (m *Manager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// In a real rotation scenario, we'd look up key by kid
		return m.signingKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
