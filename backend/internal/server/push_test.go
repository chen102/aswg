package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

type mockPushSink struct {
	ch chan pushEnvelope
}

func (m *mockPushSink) Name() string { return "mock" }

func (m *mockPushSink) Send(_ context.Context, payload pushEnvelope) error {
	m.ch <- payload
	return nil
}

func TestBuildPushEnvelope(t *testing.T) {
	ev := model.SessionEvent{
		Adapter:   "picoclaw",
		SessionID: "pico_sess_1",
		Seq:       12,
		Ts:        time.Now().UTC(),
		Type:      "message.done",
		Normalized: map[string]any{
			"role": "assistant",
			"done": true,
			"text": "你好，世界",
		},
	}
	envelope, ok := buildPushEnvelope(ev)
	if !ok {
		t.Fatalf("expected push candidate")
	}
	if envelope.Notification.Adapter != ev.Adapter {
		t.Fatalf("adapter mismatch")
	}
	if envelope.Notification.SessionID != ev.SessionID {
		t.Fatalf("session mismatch")
	}
	if envelope.Notification.Preview == "" {
		t.Fatalf("preview should not be empty")
	}
}

func TestBuildPushEnvelopeIgnoresUserEvent(t *testing.T) {
	ev := model.SessionEvent{
		Adapter:   "codex",
		SessionID: "sess_1",
		Seq:       1,
		Ts:        time.Now().UTC(),
		Type:      "message.user",
		Normalized: map[string]any{
			"role": "user",
			"done": true,
			"text": "hello",
		},
	}
	_, ok := buildPushEnvelope(ev)
	if ok {
		t.Fatalf("user event should not produce push envelope")
	}
}

func TestBuildPushEnvelopeAcceptsDeltaWithMessageID(t *testing.T) {
	ev := model.SessionEvent{
		Adapter:   "picoclaw",
		SessionID: "pico_sess_x",
		Seq:       5,
		Ts:        time.Now().UTC(),
		Type:      "message.delta",
		Payload: map[string]any{
			"message_id": "m-1",
		},
		Normalized: map[string]any{
			"role": "assistant",
			"text": "提醒已设置",
			"done": false,
		},
	}
	envelope, ok := buildPushEnvelope(ev)
	if !ok {
		t.Fatalf("expected delta push candidate with message_id")
	}
	if envelope.Notification.ID != "picoclaw:pico_sess_x:msg:m-1" {
		t.Fatalf("unexpected id: %s", envelope.Notification.ID)
	}
}

func TestBuildPushTargetURL(t *testing.T) {
	full := buildPushTargetURL(pushNotification{Adapter: "picoclaw", SessionID: "pico_sess_1"})
	if full != "/?adapter=picoclaw&session_id=pico_sess_1" {
		t.Fatalf("unexpected full url: %s", full)
	}

	adapterOnly := buildPushTargetURL(pushNotification{Adapter: "codex"})
	if adapterOnly != "/?adapter=codex" {
		t.Fatalf("unexpected adapter-only url: %s", adapterOnly)
	}

	sessionOnly := buildPushTargetURL(pushNotification{SessionID: "019cfabc"})
	if sessionOnly != "/?session_id=019cfabc" {
		t.Fatalf("unexpected session-only url: %s", sessionOnly)
	}

	empty := buildPushTargetURL(pushNotification{})
	if empty != "/" {
		t.Fatalf("unexpected empty url: %s", empty)
	}
}

func TestPushDispatcherDedupe(t *testing.T) {
	sink := &mockPushSink{ch: make(chan pushEnvelope, 4)}
	d := &pushDispatcher{
		enabled:   true,
		queue:     make(chan pushEnvelope, 8),
		sinks:     []pushSink{sink},
		dedupeTTL: time.Minute,
		seen:      make(map[string]time.Time),
		deltaAt:   make(map[string]time.Time),
		deltaGap:  100 * time.Millisecond,
	}
	go d.loop()
	defer close(d.queue)

	ev := model.SessionEvent{
		Adapter:   "picoclaw",
		SessionID: "pico_sess_2",
		Seq:       99,
		Ts:        time.Now().UTC(),
		Type:      "message.done",
		Normalized: map[string]any{
			"role": "assistant",
			"done": true,
			"text": "提醒消息",
		},
	}

	d.Publish(ev)
	d.Publish(ev)

	timeout := time.After(2 * time.Second)
	got := 0
	for got < 2 {
		select {
		case <-sink.ch:
			got++
		case <-timeout:
			if got != 1 {
				t.Fatalf("expected exactly 1 pushed message, got %d", got)
			}
			return
		}
	}
	t.Fatalf("expected at most one pushed message, got %d", got)
}

func TestPushDispatcherThrottleDelta(t *testing.T) {
	sink := &mockPushSink{ch: make(chan pushEnvelope, 4)}
	d := &pushDispatcher{
		enabled:   true,
		queue:     make(chan pushEnvelope, 8),
		sinks:     []pushSink{sink},
		dedupeTTL: time.Minute,
		seen:      make(map[string]time.Time),
		deltaAt:   make(map[string]time.Time),
		deltaGap:  500 * time.Millisecond,
		deltaTTL:  500 * time.Millisecond,
	}
	go d.loop()
	defer close(d.queue)

	base := model.SessionEvent{
		Adapter:   "picoclaw",
		SessionID: "pico_sess_8",
		Ts:        time.Now().UTC(),
		Type:      "message.delta",
		Normalized: map[string]any{
			"role": "assistant",
			"text": "chunk",
			"done": false,
		},
	}
	ev1 := base
	ev1.Seq = 10
	ev2 := base
	ev2.Seq = 11

	d.Publish(ev1)
	d.Publish(ev2)

	timeout := time.After(2 * time.Second)
	got := 0
	for {
		select {
		case <-sink.ch:
			got++
		case <-timeout:
			if got != 1 {
				t.Fatalf("expected throttled delta pushes=1, got %d", got)
			}
			return
		}
	}
}

func TestPushDispatcherDeltaCleanup(t *testing.T) {
	d := &pushDispatcher{
		enabled:  true,
		deltaAt:  map[string]time.Time{"old::session": time.Now().Add(-2 * time.Second)},
		deltaGap: 200 * time.Millisecond,
		deltaTTL: 500 * time.Millisecond,
	}
	throttled := d.shouldThrottleDelta(pushNotification{
		Event:     "session.message.delta",
		Adapter:   "picoclaw",
		SessionID: "pico_sess_clean",
	})
	if throttled {
		t.Fatalf("expected first delta not throttled")
	}
	if _, ok := d.deltaAt["old::session"]; ok {
		t.Fatalf("expected stale delta key to be cleaned")
	}
}

func TestSessionPresenceTracker(t *testing.T) {
	p := newSessionPresenceTracker()
	adapterName := "picoclaw"
	sessionID := "pico_sess_3"
	if p.Has(adapterName, sessionID) {
		t.Fatalf("expected empty presence")
	}
	p.Add(adapterName, sessionID)
	if !p.Has(adapterName, sessionID) {
		t.Fatalf("expected session present after add")
	}
	p.Remove(adapterName, sessionID)
	if p.Has(adapterName, sessionID) {
		t.Fatalf("expected session absent after remove")
	}
}

func TestWebPushSubscriptionStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webpush-subscriptions.json")
	store, err := newWebPushSubscriptionStore(path)
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore() error = %v", err)
	}

	rec, err := store.Upsert(webPushSubscriptionRequest{
		Action: "subscribe",
		Subscription: webPushSubscriptionPayload{
			Endpoint: "https://example.test/push/1",
			Keys: webPushSubscriptionKeys{
				P256DH: "p256dh",
				Auth:   "auth",
			},
		},
		Adapter:   "codex",
		SessionID: "sess-1",
		Source:    "test",
		UserAgent: "ua",
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if rec.ID == "" {
		t.Fatalf("expected record id")
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected one record")
	}

	storeReloaded, err := newWebPushSubscriptionStore(path)
	if err != nil {
		t.Fatalf("reload store error = %v", err)
	}
	if len(storeReloaded.List()) != 1 {
		t.Fatalf("expected one persisted record")
	}
	removed, err := storeReloaded.RemoveByEndpoint("https://example.test/push/1")
	if err != nil {
		t.Fatalf("RemoveByEndpoint() error = %v", err)
	}
	if !removed {
		t.Fatalf("expected removed=true")
	}
	if len(storeReloaded.List()) != 0 {
		t.Fatalf("expected no records after remove")
	}
}

func TestPushSubscriptionEndpoints(t *testing.T) {
	registry := adapter.NewRegistry("codex")
	srv := New(Config{
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 30,
		FrontendDir:             "../frontend/src",
		WebPushSubscriptionFile: filepath.Join(t.TempDir(), "subs.json"),
	}, registry)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]any{
		"action": "subscribe",
		"subscription": map[string]any{
			"endpoint": "https://example.test/push/2",
			"keys": map[string]any{
				"p256dh": "p256dh",
				"auth":   "auth",
			},
			"expirationTime": nil,
		},
		"adapter":    "codex",
		"session_id": "sess-2",
		"source":     "aswg-web",
		"user_agent": "ua",
	}
	body, _ := json.Marshal(payload)
	resp, err := ts.Client().Post(ts.URL+"/api/v1/push/subscriptions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("subscribe endpoint error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status=%d", resp.StatusCode)
	}

	removePayload := map[string]any{
		"action": "unsubscribe",
		"subscription": map[string]any{
			"endpoint": "https://example.test/push/2",
			"keys": map[string]any{
				"p256dh": "p256dh",
				"auth":   "auth",
			},
		},
	}
	removeBody, _ := json.Marshal(removePayload)
	resp2, err := ts.Client().Post(ts.URL+"/api/v1/push/subscriptions/remove", "application/json", bytes.NewReader(removeBody))
	if err != nil {
		t.Fatalf("remove endpoint error = %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("remove status=%d", resp2.StatusCode)
	}
}
