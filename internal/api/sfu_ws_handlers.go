package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/technosupport/ts-vms/internal/tokens"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for dev; restrict in prod
	},
}

type SfuWsHandler struct {
	Tokens *tokens.Manager
	// In the future, we'd have a Hub to broadcast events.
	// For now, we just accept connections and log messages.
	// We might need to forward Candidates to SFU via NATS or HTTP.
	// But Phase 3.4 says "HTTP REST sufficient for ICE Lite".
	// The prompt implies we DO implement WS.
	// "Add WS endpoint for ICE candidates"
	// So we should accept them.
}

func NewSfuWsHandler(tm *tokens.Manager) *SfuWsHandler {
	return &SfuWsHandler{Tokens: tm}
}

func (h *SfuWsHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// 1. Auth via Query Param (standard for WS)
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := h.Tokens.ValidateToken(tokenStr)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// 2. Upgrade
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS Upgrade Failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WS Connected: User=%s Tenant=%s", claims.UserID, claims.TenantID)

	// 3. Loop
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WS Read Error: %v", err)
			break
		}

		// Handle Message
		var payload map[string]interface{}
		if err := json.Unmarshal(msg, &payload); err != nil {
			log.Printf("WS Json Error: %v", err)
			continue
		}

		eventType, _ := payload["type"].(string)

		switch eventType {
		case "connection-state":
			state, _ := payload["state"].(string)
			log.Printf("WS [%s] Connection State: %s", claims.UserID, state)
		case "candidate":
			// Process ICE Candidate
			// For ICE Lite (SFU), we usually don't need client candidates unless we support trickle.
			// Mediasoup WebRtcTransport supports it.
			// We would need to forward this to SFU.
			// "Deferred: HTTP REST sufficient for ICE Lite".
			// But prompt says "Add WS endpoint for ICE candidates".
			// So we Log it for now.
			cand, _ := payload["candidate"].(map[string]interface{})
			log.Printf("WS [%s] Candidate: %v", claims.UserID, cand)
		default:
			log.Printf("WS [%s] Unknown: %s", claims.UserID, eventType)
		}
	}
}
