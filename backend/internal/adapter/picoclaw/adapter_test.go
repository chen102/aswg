package picoclaw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestDiscoverSessionsImportsHistory(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")
	t.Setenv("PICOCLAW_HISTORY_ENABLED", "true")

	historyDir := t.TempDir()
	t.Setenv("PICOCLAW_HISTORY_DIR", historyDir)

	historyFile := filepath.Join(historyDir, "agent_main_pico_direct_pico_hist_test_001.json")
	payload := `{
  "key": "agent:main:pico:direct:pico:hist_test_001",
  "messages": [
    {"role":"user","content":"历史会话测试问题"},
    {"role":"assistant","content":"历史会话测试回答"}
  ],
  "created": "2026-03-16T09:00:00.000Z",
  "updated": "2026-03-16T09:00:03.000Z"
}`
	if err := os.WriteFile(historyFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write history file error = %v", err)
	}

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	page, err := a.DiscoverSessions(context.Background(), model.DiscoverRequest{Limit: 20})
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	found := false
	for _, item := range page.Items {
		if item.ID == "hist_test_001" {
			found = true
			if item.Title != "历史会话测试问题" {
				t.Fatalf("unexpected imported title: %q", item.Title)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected imported history session in discover list")
	}

	events, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: "hist_test_001",
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(events.Items) != 2 {
		t.Fatalf("expected 2 imported events, got %d", len(events.Items))
	}
	if events.Items[0].Type != "message.user" {
		t.Fatalf("event[0] expected message.user, got %s", events.Items[0].Type)
	}
	if text, _ := events.Items[0].Normalized["text"].(string); text != "历史会话测试问题" {
		t.Fatalf("event[0] expected text=%q, got %q", "历史会话测试问题", text)
	}
	if events.Items[1].Type != "message.done" {
		t.Fatalf("event[1] expected message.done, got %s", events.Items[1].Type)
	}
	if text, _ := events.Items[1].Normalized["text"].(string); text != "历史会话测试回答" {
		t.Fatalf("event[1] expected text=%q, got %q", "历史会话测试回答", text)
	}
}

func TestSubscribeBridgesLivePicoMessage(t *testing.T) {
	const token = "pico-token"
	const pushedText = "后台推送：你好"

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

		sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
		if sessionID == "" {
			return
		}

		_ = conn.WriteJSON(picoMessage{
			Type:      picoTypeMessageCreate,
			SessionID: sessionID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"message_id": "live-msg-1",
				"content":    pushedText,
			},
		})
		time.Sleep(120 * time.Millisecond)
	}))
	defer server.Close()

	t.Setenv("PICOCLAW_TOKEN", token)
	t.Setenv("PICOCLAW_WS_BASE_URL", server.URL)
	t.Setenv("PICOCLAW_HISTORY_ENABLED", "false")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{Title: "live-bridge"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	ch, unsubscribe, err := a.Subscribe(context.Background(), detail.ID, 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "message.done" {
				continue
			}
			text, _ := ev.Normalized["text"].(string)
			if text != pushedText {
				continue
			}
			return
		case <-timeout:
			t.Fatalf("timeout waiting live bridged event")
		}
	}
}

func TestHandleLiveMessageTracksPassiveSessionStatus(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")
	t.Setenv("PICOCLAW_HISTORY_ENABLED", "false")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{Title: "status-check"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	typing := false
	messageCache := map[string]string{}

	a.handleLiveMessage(detail.ID, &typing, messageCache, picoMessage{
		Type:      picoTypeTypingStart,
		SessionID: detail.ID,
	})
	if !typing {
		t.Fatalf("expected typing=true after typing.start")
	}
	runningDetail, err := a.GetSession(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if runningDetail.Status != sessionStatusRun {
		t.Fatalf("expected status=%q after typing.start, got %q", sessionStatusRun, runningDetail.Status)
	}

	a.handleLiveMessage(detail.ID, &typing, messageCache, picoMessage{
		Type:      picoTypeMessageCreate,
		SessionID: detail.ID,
		Payload: map[string]any{
			"message_id": "msg-1",
			"content":    "hello",
		},
	})
	runningDetail, err = a.GetSession(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if runningDetail.Status != sessionStatusRun {
		t.Fatalf("expected status=%q while streaming, got %q", sessionStatusRun, runningDetail.Status)
	}

	a.handleLiveMessage(detail.ID, &typing, messageCache, picoMessage{
		Type:      picoTypeTypingStop,
		SessionID: detail.ID,
	})
	if typing {
		t.Fatalf("expected typing=false after typing.stop")
	}
	idleDetail, err := a.GetSession(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if idleDetail.Status != sessionStatusIdle {
		t.Fatalf("expected status=%q after typing.stop, got %q", sessionStatusIdle, idleDetail.Status)
	}
}

func TestMessageBelongsToSession(t *testing.T) {
	sessionID := "pico_sess_target_001"
	tests := []struct {
		name string
		msg  picoMessage
		want bool
	}{
		{
			name: "match by top-level session id",
			msg: picoMessage{
				SessionID: sessionID,
			},
			want: true,
		},
		{
			name: "mismatch by top-level session id",
			msg: picoMessage{
				SessionID: "pico_sess_other",
			},
			want: false,
		},
		{
			name: "match by payload session_id",
			msg: picoMessage{
				Payload: map[string]any{"session_id": sessionID},
			},
			want: true,
		},
		{
			name: "match by payload chat_id",
			msg: picoMessage{
				Payload: map[string]any{"chat_id": "pico:pico:" + sessionID},
			},
			want: true,
		},
		{
			name: "mismatch by payload chat_id",
			msg: picoMessage{
				Payload: map[string]any{"chat_id": "pico:pico:pico_sess_other"},
			},
			want: false,
		},
		{
			name: "no session info keeps compatibility",
			msg: picoMessage{
				Payload: map[string]any{"content": "hello"},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := messageBelongsToSession(tc.msg, sessionID)
			if got != tc.want {
				t.Fatalf("messageBelongsToSession()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleLiveMessageDedupesImmediateDoneReplay(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")
	t.Setenv("PICOCLAW_HISTORY_ENABLED", "false")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{Title: "dedupe-check"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	typing := false
	messageCache := map[string]string{}
	msg := picoMessage{
		Type:      picoTypeMessageCreate,
		SessionID: detail.ID,
		Payload: map[string]any{
			"content": "same text",
		},
	}
	a.handleLiveMessage(detail.ID, &typing, messageCache, msg)
	a.handleLiveMessage(detail.ID, &typing, messageCache, msg)

	events, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: detail.ID,
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	doneCount := 0
	for _, ev := range events.Items {
		if ev.Type != "message.done" {
			continue
		}
		text, _ := ev.Normalized["text"].(string)
		if text == "same text" {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Fatalf("expected 1 deduped message.done, got %d", doneCount)
	}
}

func TestFinishSessionRunAddsLiveSuppressWindow(t *testing.T) {
	t.Setenv("PICOCLAW_TOKEN", "test-token")
	t.Setenv("PICOCLAW_WS_BASE_URL", "ws://127.0.0.1:65535")
	t.Setenv("PICOCLAW_HISTORY_ENABLED", "false")

	a, err := NewAdapter()
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{Title: "suppress-check"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	a.mu.Lock()
	if s, ok := a.sessions[detail.ID]; ok {
		s.activeRuns = 1
	}
	a.mu.Unlock()

	a.finishSessionRun(detail.ID)
	if !a.sessionShouldSuppressLive(detail.ID) {
		t.Fatalf("expected live suppression window right after finishSessionRun")
	}
}

func TestShouldAttemptGatewayAutostart(t *testing.T) {
	if !shouldAttemptGatewayAutostart("ws://127.0.0.1:18790", errors.New("dial tcp 127.0.0.1:18790: connect: connection refused")) {
		t.Fatalf("expected autostart on local connection refused")
	}
	if shouldAttemptGatewayAutostart("ws://10.0.0.2:18790", errors.New("dial tcp 10.0.0.2:18790: connect: connection refused")) {
		t.Fatalf("did not expect autostart on non-local ws base")
	}
	if shouldAttemptGatewayAutostart("ws://127.0.0.1:18790", errors.New("remote handshake failed")) {
		t.Fatalf("did not expect autostart for non-refused errors")
	}
}

func TestWSBaseTCPAddr(t *testing.T) {
	got, err := wsBaseTCPAddr("ws://127.0.0.1:18790")
	if err != nil {
		t.Fatalf("wsBaseTCPAddr() error = %v", err)
	}
	if got != "127.0.0.1:18790" {
		t.Fatalf("wsBaseTCPAddr() = %q, want %q", got, "127.0.0.1:18790")
	}

	got, err = wsBaseTCPAddr("ws://0.0.0.0:18790")
	if err != nil {
		t.Fatalf("wsBaseTCPAddr() error = %v", err)
	}
	if got != "127.0.0.1:18790" {
		t.Fatalf("wsBaseTCPAddr() with wildcard host = %q, want %q", got, "127.0.0.1:18790")
	}
}

func TestResolveGatewayBinaryWithAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "picoclaw")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary error = %v", err)
	}
	got, err := resolveGatewayBinary(bin)
	if err != nil {
		t.Fatalf("resolveGatewayBinary() error = %v", err)
	}
	if got != bin {
		t.Fatalf("resolveGatewayBinary() = %q, want %q", got, bin)
	}
}
