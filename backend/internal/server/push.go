package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

const pushEnvelopeVersion = "v1"

type pushNotification struct {
	ID        string    `json:"id"`
	Event     string    `json:"event"`
	Adapter   string    `json:"adapter"`
	SessionID string    `json:"session_id"`
	Seq       int64     `json:"seq"`
	Ts        time.Time `json:"ts"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	Preview   string    `json:"preview"`
}

type pushEnvelope struct {
	Version      string           `json:"version"`
	Notification pushNotification `json:"notification"`
}

type pushSink interface {
	Name() string
	Send(ctx context.Context, payload pushEnvelope) error
}

type pushDispatcher struct {
	enabled     bool
	queue       chan pushEnvelope
	sinks       []pushSink
	sendTimeout time.Duration
	dedupeTTL   time.Duration
	seenMu      sync.Mutex
	seen        map[string]time.Time
	deltaMu     sync.Mutex
	deltaAt     map[string]time.Time
	deltaGap    time.Duration
	deltaTTL    time.Duration
}

func newPushDispatcher(cfg Config) *pushDispatcher {
	url := strings.TrimSpace(cfg.PushWebhookURL)
	if url == "" {
		return &pushDispatcher{}
	}

	queueSize := cfg.PushQueueSize
	if queueSize <= 0 {
		queueSize = 512
	}
	ttl := cfg.PushDedupeTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	timeout := cfg.PushWebhookTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	sink := &webhookPushSink{
		url:        url,
		authBearer: strings.TrimSpace(cfg.PushWebhookAuthBearer),
		hmacSecret: strings.TrimSpace(cfg.PushWebhookHMACSecret),
		client: &http.Client{
			Timeout: timeout,
		},
	}

	d := &pushDispatcher{
		enabled:     true,
		queue:       make(chan pushEnvelope, queueSize),
		sinks:       []pushSink{sink},
		sendTimeout: timeout,
		dedupeTTL:   ttl,
		seen:        make(map[string]time.Time, queueSize),
		deltaAt:     make(map[string]time.Time),
		deltaGap:    8 * time.Second,
		deltaTTL:    10 * time.Minute,
	}
	if d.deltaTTL < d.deltaGap*3 {
		d.deltaTTL = d.deltaGap * 3
	}
	go d.loop()
	log.Printf("push dispatcher enabled: sink=%s queue=%d", sink.Name(), queueSize)
	return d
}

func (d *pushDispatcher) Enabled() bool {
	return d != nil && d.enabled
}

func (d *pushDispatcher) Publish(ev model.SessionEvent) {
	if !d.Enabled() {
		return
	}
	envelope, ok := buildPushEnvelope(ev)
	if !ok {
		return
	}
	if d.isDuplicate(envelope.Notification.ID) {
		return
	}
	if d.shouldThrottleDelta(envelope.Notification) {
		return
	}
	select {
	case d.queue <- envelope:
	default:
		log.Printf("push dispatcher queue full, drop notification: id=%s", envelope.Notification.ID)
	}
}

func (d *pushDispatcher) loop() {
	for payload := range d.queue {
		for _, sink := range d.sinks {
			ctx := context.Background()
			cancel := func() {}
			if d.sendTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, d.sendTimeout)
			}
			err := sink.Send(ctx, payload)
			cancel()
			if err != nil {
				log.Printf("push sink send failed: sink=%s id=%s err=%v", sink.Name(), payload.Notification.ID, err)
			}
		}
	}
}

func (d *pushDispatcher) isDuplicate(id string) bool {
	now := time.Now()
	d.seenMu.Lock()
	defer d.seenMu.Unlock()

	for key, at := range d.seen {
		if now.Sub(at) > d.dedupeTTL {
			delete(d.seen, key)
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}

func (d *pushDispatcher) shouldThrottleDelta(n pushNotification) bool {
	if n.Event != "session.message.delta" {
		return false
	}
	key := presenceKey(n.Adapter, n.SessionID)
	if key == "" {
		return false
	}
	now := time.Now()
	d.deltaMu.Lock()
	defer d.deltaMu.Unlock()
	for k, at := range d.deltaAt {
		if now.Sub(at) > d.deltaTTL {
			delete(d.deltaAt, k)
		}
	}
	last := d.deltaAt[key]
	if !last.IsZero() && now.Sub(last) < d.deltaGap {
		return true
	}
	d.deltaAt[key] = now
	return false
}

func buildPushEnvelope(ev model.SessionEvent) (pushEnvelope, bool) {
	role := strings.TrimSpace(strings.ToLower(anyToString(ev.Normalized["role"])))
	if role != "assistant" {
		return pushEnvelope{}, false
	}

	done := false
	if v, ok := ev.Normalized["done"].(bool); ok {
		done = v
	}

	text := strings.TrimSpace(anyToString(ev.Normalized["text"]))
	if text == "" {
		text = strings.TrimSpace(anyToString(ev.Payload["text"]))
	}
	if text == "" {
		return pushEnvelope{}, false
	}

	var id string
	eventName := "session.message.done"
	switch {
	case done || ev.Type == "message.done":
		id = fmt.Sprintf("%s:%s:%d", strings.TrimSpace(ev.Adapter), strings.TrimSpace(ev.SessionID), ev.Seq)
	case ev.Type == "message.delta":
		eventName = "session.message.delta"
		// Some channels (e.g. Pico) may emit only delta text while final done
		// events are empty; use stable message_id if present.
		messageID := strings.TrimSpace(anyToString(ev.Payload["message_id"]))
		if messageID != "" {
			id = fmt.Sprintf("%s:%s:msg:%s", strings.TrimSpace(ev.Adapter), strings.TrimSpace(ev.SessionID), messageID)
		} else {
			id = fmt.Sprintf("%s:%s:%d", strings.TrimSpace(ev.Adapter), strings.TrimSpace(ev.SessionID), ev.Seq)
		}
	default:
		return pushEnvelope{}, false
	}

	return pushEnvelope{
		Version: pushEnvelopeVersion,
		Notification: pushNotification{
			ID:        id,
			Event:     eventName,
			Adapter:   strings.TrimSpace(ev.Adapter),
			SessionID: strings.TrimSpace(ev.SessionID),
			Seq:       ev.Seq,
			Ts:        ev.Ts,
			Role:      role,
			Text:      text,
			Preview:   truncateText(text, 120),
		},
	}, true
}

func truncateText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return ""
	}
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "..."
}

type webhookPushSink struct {
	url        string
	authBearer string
	hmacSecret string
	client     *http.Client
}

func (w *webhookPushSink) Name() string { return "webhook" }

func (w *webhookPushSink) Send(ctx context.Context, payload pushEnvelope) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "aswg-push/"+pushEnvelopeVersion)
	req.Header.Set("X-ASWG-Event-ID", payload.Notification.ID)
	if w.authBearer != "" {
		req.Header.Set("Authorization", "Bearer "+w.authBearer)
	}
	if w.hmacSecret != "" {
		mac := hmac.New(sha256.New, []byte(w.hmacSecret))
		_, _ = mac.Write(body)
		req.Header.Set("X-ASWG-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status=%d", resp.StatusCode)
	}
	return nil
}

type sessionPresenceTracker struct {
	mu     sync.RWMutex
	counts map[string]int
}

func newSessionPresenceTracker() *sessionPresenceTracker {
	return &sessionPresenceTracker{
		counts: make(map[string]int),
	}
}

func (p *sessionPresenceTracker) Add(adapterName, sessionID string) {
	key := presenceKey(adapterName, sessionID)
	if key == "" {
		return
	}
	p.mu.Lock()
	p.counts[key]++
	p.mu.Unlock()
}

func (p *sessionPresenceTracker) Remove(adapterName, sessionID string) {
	key := presenceKey(adapterName, sessionID)
	if key == "" {
		return
	}
	p.mu.Lock()
	n := p.counts[key]
	if n <= 1 {
		delete(p.counts, key)
	} else {
		p.counts[key] = n - 1
	}
	p.mu.Unlock()
}

func (p *sessionPresenceTracker) Has(adapterName, sessionID string) bool {
	key := presenceKey(adapterName, sessionID)
	if key == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.counts[key] > 0
}

func presenceKey(adapterName, sessionID string) string {
	adapterName = strings.TrimSpace(adapterName)
	sessionID = strings.TrimSpace(sessionID)
	if adapterName == "" || sessionID == "" {
		return ""
	}
	return adapterName + "::" + sessionID
}

func (s *Server) attachAdapterEventObservers() {
	if s.registry == nil {
		return
	}
	for _, info := range s.registry.List() {
		a, ok := s.registry.Get(info.Name)
		if !ok || a == nil {
			continue
		}
		capable, ok := a.(adapter.EventObserverCapable)
		if !ok {
			continue
		}
		capable.SetEventObserver(s.handleAdapterEvent)
	}
}

func (s *Server) handleAdapterEvent(ev model.SessionEvent) {
	if s.pushDispatcher == nil || !s.pushDispatcher.Enabled() {
		return
	}
	if s.wsPresence != nil && s.wsPresence.Has(ev.Adapter, ev.SessionID) {
		return
	}
	s.pushDispatcher.Publish(ev)
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}
