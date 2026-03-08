package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type picoMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	host := envOrDefault("MOCK_PICO_HOST", "127.0.0.1")
	port := envIntOrDefault("MOCK_PICO_PORT", 18081)
	token := envOrDefault("MOCK_PICO_TOKEN", "pico-dev-token")
	allowTokenQuery := envBoolOrDefault("MOCK_PICO_ALLOW_TOKEN_QUERY", true)
	chunkDelay := time.Duration(envIntOrDefault("MOCK_PICO_CHUNK_DELAY_MS", 120)) * time.Millisecond

	mux := http.NewServeMux()
	mux.HandleFunc("/pico/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWS(w, r, token, allowTokenQuery, chunkDelay)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("mock pico server started on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock pico server failed: %v", err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request, token string, allowTokenQuery bool, chunkDelay time.Duration) {
	if !authenticate(r, token, allowTokenQuery) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		sessionID = fmt.Sprintf("mock_sess_%d", time.Now().UnixNano())
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg picoMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			_ = conn.WriteJSON(newError("invalid_message", "failed to parse message", sessionID))
			continue
		}

		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "ping":
			_ = conn.WriteJSON(picoMessage{
				Type:      "pong",
				ID:        msg.ID,
				SessionID: sessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		case "message.send":
			content := extractContent(msg.Payload)
			if content == "" {
				_ = conn.WriteJSON(newError("empty_content", "message content is empty", sessionID))
				continue
			}
			if msg.SessionID != "" {
				sessionID = strings.TrimSpace(msg.SessionID)
			}
			streamMockReply(conn, sessionID, content, chunkDelay)
		default:
			_ = conn.WriteJSON(newError("unknown_type", "unknown message type", sessionID))
		}
	}
}

func streamMockReply(conn *websocket.Conn, sessionID, userPrompt string, chunkDelay time.Duration) {
	messageID := fmt.Sprintf("mock_msg_%d", time.Now().UnixNano())

	_ = conn.WriteJSON(picoMessage{
		Type:      "typing.start",
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
	})

	reply := "Mock Pico 已收到并处理: " + strings.TrimSpace(userPrompt)
	chunks := splitByRunes(reply, 8)
	accumulated := ""
	for idx, chunk := range chunks {
		accumulated += chunk
		typ := "message.update"
		if idx == 0 {
			typ = "message.create"
		}
		_ = conn.WriteJSON(picoMessage{
			Type:      typ,
			SessionID: sessionID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"message_id": messageID,
				"content":    accumulated,
			},
		})
		if chunkDelay > 0 {
			time.Sleep(chunkDelay)
		}
	}

	_ = conn.WriteJSON(picoMessage{
		Type:      "typing.stop",
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
	})
}

func splitByRunes(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if width <= 0 {
		return []string{text}
	}

	runes := []rune(text)
	out := make([]string, 0, len(runes)/width+1)
	for i := 0; i < len(runes); i += width {
		end := i + width
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func authenticate(r *http.Request, token string, allowTokenQuery bool) bool {
	if token == "" {
		return false
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		if strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == token {
			return true
		}
	}
	if allowTokenQuery && strings.TrimSpace(r.URL.Query().Get("token")) == token {
		return true
	}
	return false
}

func extractContent(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if content, ok := payload["content"].(string); ok {
		return strings.TrimSpace(content)
	}
	if content, ok := payload["text"].(string); ok {
		return strings.TrimSpace(content)
	}
	return ""
}

func newError(code, message, sessionID string) picoMessage {
	return picoMessage{
		Type:      "error",
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"code":    code,
			"message": message,
		},
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envBoolOrDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
