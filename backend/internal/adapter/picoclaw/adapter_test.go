package picoclaw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"agent-session-web-gateway/backend/internal/model"
)

func TestCreateDiscoverAndEvents(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{
		Title:      "Session A",
		Workspace:  "/tmp/work",
		SeedPrompt: "seed",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if detail.ID == "" {
		t.Fatalf("expected session id")
	}

	got, err := a.GetSession(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.Title != "Session A" {
		t.Fatalf("unexpected title: %s", got.Title)
	}

	events, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: detail.ID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(events.Items) != 2 {
		t.Fatalf("expected 2 seed events, got %d", len(events.Items))
	}

	page, err := a.DiscoverSessions(context.Background(), model.DiscoverRequest{Limit: 20})
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	found := false
	for _, item := range page.Items {
		if item.ID == detail.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session %s not found in discover list", detail.ID)
	}
}

func TestContinueSessionValidation(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = a.ContinueSession(context.Background(), model.ContinueInput{SessionID: detail.ID, Prompt: "   "})
	if !errors.Is(err, model.ErrInvalidParam) {
		t.Fatalf("expected ErrInvalidParam, got %v", err)
	}
}

func TestContinueSessionMapsPicoEvents(t *testing.T) {
	const token = "pico-token"

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pico/ws" {
			http.NotFound(w, r)
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg picoMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return
		}
		if msg.Type != picoTypeMessageSend {
			return
		}

		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID = r.URL.Query().Get("session_id")
		}

		now := time.Now().UnixMilli()
		_ = conn.WriteJSON(picoMessage{Type: picoTypeTypingStart, SessionID: sessionID, Timestamp: now})
		_ = conn.WriteJSON(picoMessage{
			Type:      picoTypeMessageCreate,
			SessionID: sessionID,
			Timestamp: now + 1,
			Payload: map[string]any{
				"message_id": "m1",
				"content":    "你好",
			},
		})
		_ = conn.WriteJSON(picoMessage{
			Type:      picoTypeMessageUpdate,
			SessionID: sessionID,
			Timestamp: now + 2,
			Payload: map[string]any{
				"message_id": "m1",
				"content":    "你好，世界",
			},
		})
		_ = conn.WriteJSON(picoMessage{Type: picoTypeTypingStop, SessionID: sessionID, Timestamp: now + 3})
	}))
	defer server.Close()

	t.Setenv("PICOCLAW_TOKEN", token)
	t.Setenv("PICOCLAW_WS_BASE_URL", server.URL)
	t.Setenv("PICOCLAW_CONTINUE_TIMEOUT_MS", "10000")
	t.Setenv("PICOCLAW_READ_IDLE_TIMEOUT_MS", "5000")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID: detail.ID,
		Prompt:    "你好",
	})
	if err != nil {
		t.Fatalf("ContinueSession() error = %v", err)
	}

	events := waitForDoneEvents(t, a, detail.ID, 3*time.Second)
	if len(events) < 5 {
		t.Fatalf("expected at least 5 events, got %d", len(events))
	}

	if events[0].Type != "message.user" {
		t.Fatalf("event[0] expected message.user, got %s", events[0].Type)
	}
	if events[1].Type != "message.action" {
		t.Fatalf("event[1] expected message.action, got %s", events[1].Type)
	}
	if events[2].Type != "message.delta" {
		t.Fatalf("event[2] expected message.delta, got %s", events[2].Type)
	}
	if text, _ := events[2].Normalized["text"].(string); text != "你好" {
		t.Fatalf("event[2] expected delta text=你好, got %q", text)
	}
	if events[3].Type != "message.delta" {
		t.Fatalf("event[3] expected message.delta, got %s", events[3].Type)
	}
	if text, _ := events[3].Normalized["text"].(string); text != "，世界" {
		t.Fatalf("event[3] expected delta text=，世界, got %q", text)
	}
	if events[4].Type != "message.done" {
		t.Fatalf("event[4] expected message.done, got %s", events[4].Type)
	}
}

func waitForDoneEvents(t *testing.T, a *Adapter, sessionID string, timeout time.Duration) []model.SessionEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		page, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
			SessionID: sessionID,
			Limit:     model.MaxEventsLimit,
		})
		if err != nil {
			t.Fatalf("GetSessionEvents() error = %v", err)
		}
		for _, ev := range page.Items {
			if ev.Type == "message.done" {
				return page.Items
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for message.done")
	return nil
}
