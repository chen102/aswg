package server

import (
	"context"
	"testing"
	"time"

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
