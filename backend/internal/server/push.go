package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"

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

type webPushSubscriptionRequest struct {
	Action       string                     `json:"action,omitempty"`
	Subscription webPushSubscriptionPayload `json:"subscription"`
	Endpoint     string                     `json:"endpoint,omitempty"`
	Adapter      string                     `json:"adapter,omitempty"`
	SessionID    string                     `json:"session_id,omitempty"`
	Source       string                     `json:"source,omitempty"`
	Ts           string                     `json:"ts,omitempty"`
	UserAgent    string                     `json:"user_agent,omitempty"`
}

type webPushSubscriptionPayload struct {
	Endpoint       string                  `json:"endpoint"`
	Keys           webPushSubscriptionKeys `json:"keys"`
	ExpirationTime any                     `json:"expirationTime,omitempty"`
}

type webPushSubscriptionKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type webPushSubscriptionRecord struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	P256DH    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	Adapter   string    `json:"adapter,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Source    string    `json:"source,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type webPushSubscriptionSnapshot struct {
	Items []webPushSubscriptionRecord `json:"items"`
}

type webPushSubscriptionStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]webPushSubscriptionRecord
}

func newWebPushSubscriptionStore(path string) (*webPushSubscriptionStore, error) {
	path = strings.TrimSpace(path)
	store := &webPushSubscriptionStore{
		path:  path,
		items: make(map[string]webPushSubscriptionRecord),
	}
	if path == "" {
		return store, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}

	var snapshot webPushSubscriptionSnapshot
	if len(bytes.TrimSpace(raw)) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	for _, item := range snapshot.Items {
		endpoint := strings.TrimSpace(item.Endpoint)
		if endpoint == "" {
			continue
		}
		item.Endpoint = endpoint
		if strings.TrimSpace(item.ID) == "" {
			item.ID = makeSubscriptionID(endpoint)
		}
		store.items[endpoint] = item
	}
	return store, nil
}

func newInMemoryWebPushSubscriptionStore() *webPushSubscriptionStore {
	return &webPushSubscriptionStore{
		items: make(map[string]webPushSubscriptionRecord),
	}
}

func (s *webPushSubscriptionStore) Upsert(req webPushSubscriptionRequest) (webPushSubscriptionRecord, error) {
	endpoint := strings.TrimSpace(req.Subscription.Endpoint)
	p256dh := strings.TrimSpace(req.Subscription.Keys.P256DH)
	auth := strings.TrimSpace(req.Subscription.Keys.Auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return webPushSubscriptionRecord{}, fmt.Errorf("invalid subscription payload")
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.items[endpoint]
	if !exists {
		rec = webPushSubscriptionRecord{
			ID:        makeSubscriptionID(endpoint),
			Endpoint:  endpoint,
			CreatedAt: now,
		}
	}
	rec.P256DH = p256dh
	rec.Auth = auth
	rec.Adapter = strings.TrimSpace(req.Adapter)
	rec.SessionID = strings.TrimSpace(req.SessionID)
	rec.Source = strings.TrimSpace(req.Source)
	rec.UserAgent = strings.TrimSpace(req.UserAgent)
	rec.UpdatedAt = now
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	s.items[endpoint] = rec

	if err := s.persistLocked(); err != nil {
		return webPushSubscriptionRecord{}, err
	}
	return rec, nil
}

func (s *webPushSubscriptionStore) RemoveByEndpoint(endpoint string) (bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[endpoint]; !ok {
		return false, nil
	}
	delete(s.items, endpoint)
	if err := s.persistLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *webPushSubscriptionStore) List() []webPushSubscriptionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]webPushSubscriptionRecord, 0, len(s.items))
	for _, rec := range s.items {
		items = append(items, rec)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].Endpoint < items[j].Endpoint
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func (s *webPushSubscriptionStore) persistLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	items := make([]webPushSubscriptionRecord, 0, len(s.items))
	for _, rec := range s.items {
		items = append(items, rec)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].Endpoint < items[j].Endpoint
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	snapshot := webPushSubscriptionSnapshot{Items: items}
	buf, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(buf, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func makeSubscriptionID(endpoint string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(endpoint)))
	return hex.EncodeToString(h[:8])
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

func newPushDispatcher(cfg Config, store *webPushSubscriptionStore) *pushDispatcher {
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

	sinks := make([]pushSink, 0, 2)
	if url := strings.TrimSpace(cfg.PushWebhookURL); url != "" {
		sinks = append(sinks, &webhookPushSink{
			url:        url,
			authBearer: strings.TrimSpace(cfg.PushWebhookAuthBearer),
			hmacSecret: strings.TrimSpace(cfg.PushWebhookHMACSecret),
			client: &http.Client{
				Timeout: timeout,
			},
		})
	}

	pub := strings.TrimSpace(cfg.WebPushVAPIDPublicKey)
	pri := strings.TrimSpace(cfg.WebPushVAPIDPrivateKey)
	subject := strings.TrimSpace(cfg.WebPushVAPIDSubject)
	if pub != "" || pri != "" || subject != "" {
		if pub == "" || pri == "" || subject == "" {
			log.Printf("web push sink disabled: WEBPUSH_VAPID_PUBLIC_KEY / WEBPUSH_VAPID_PRIVATE_KEY / WEBPUSH_VAPID_SUBJECT must all be set")
		} else if store == nil {
			log.Printf("web push sink disabled: subscription store unavailable")
		} else {
			ttlSeconds := cfg.WebPushTTLSeconds
			if ttlSeconds <= 0 {
				ttlSeconds = 60
			}
			sinks = append(sinks, &webPushSink{
				store:           store,
				vapidPublicKey:  pub,
				vapidPrivateKey: pri,
				subject:         subject,
				ttlSeconds:      ttlSeconds,
			})
		}
	}

	if len(sinks) == 0 {
		return &pushDispatcher{}
	}

	d := &pushDispatcher{
		enabled:     true,
		queue:       make(chan pushEnvelope, queueSize),
		sinks:       sinks,
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

	sinkNames := make([]string, 0, len(sinks))
	for _, sink := range sinks {
		sinkNames = append(sinkNames, sink.Name())
	}
	log.Printf("push dispatcher enabled: sinks=%s queue=%d", strings.Join(sinkNames, ","), queueSize)
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

func buildPushTargetURL(n pushNotification) string {
	adapterName := strings.TrimSpace(n.Adapter)
	sessionID := strings.TrimSpace(n.SessionID)
	if adapterName == "" && sessionID == "" {
		return "/"
	}
	query := make(url.Values)
	if adapterName != "" {
		query.Set("adapter", adapterName)
	}
	if sessionID != "" {
		query.Set("session_id", sessionID)
	}
	encoded := query.Encode()
	if encoded == "" {
		return "/"
	}
	return "/?" + encoded
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

type webPushSink struct {
	store           *webPushSubscriptionStore
	vapidPublicKey  string
	vapidPrivateKey string
	subject         string
	ttlSeconds      int
}

func (w *webPushSink) Name() string { return "webpush" }

func (w *webPushSink) Send(ctx context.Context, payload pushEnvelope) error {
	if w.store == nil {
		return nil
	}
	items := w.store.List()
	if len(items) == 0 {
		return nil
	}

	n := payload.Notification
	data, err := json.Marshal(map[string]any{
		"notification": map[string]any{
			"id":         n.ID,
			"title":      "ASWG 会话更新",
			"body":       n.Preview,
			"preview":    n.Preview,
			"adapter":    n.Adapter,
			"session_id": n.SessionID,
			"seq":        n.Seq,
			"ts":         n.Ts,
			"url":        buildPushTargetURL(n),
		},
	})
	if err != nil {
		return err
	}

	errList := make([]string, 0)
	for _, rec := range items {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !subscriptionMatchesNotification(rec, n) {
			continue
		}
		sub := &webpush.Subscription{
			Endpoint: rec.Endpoint,
			Keys: webpush.Keys{
				P256dh: rec.P256DH,
				Auth:   rec.Auth,
			},
		}
		resp, sendErr := webpush.SendNotification(data, sub, &webpush.Options{
			Subscriber:      w.subject,
			VAPIDPublicKey:  w.vapidPublicKey,
			VAPIDPrivateKey: w.vapidPrivateKey,
			TTL:             w.ttlSeconds,
		})
		if sendErr != nil {
			errList = append(errList, fmt.Sprintf("endpoint=%s err=%v", rec.ID, sendErr))
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
				_, _ = w.store.RemoveByEndpoint(rec.Endpoint)
				continue
			}
			if resp.StatusCode >= 300 {
				errList = append(errList, fmt.Sprintf("endpoint=%s status=%d", rec.ID, resp.StatusCode))
			}
		}
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, "; "))
	}
	return nil
}

func subscriptionMatchesNotification(rec webPushSubscriptionRecord, n pushNotification) bool {
	if rec.Adapter != "" && !strings.EqualFold(rec.Adapter, n.Adapter) {
		return false
	}
	return true
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
	s.pushDispatcher.Publish(ev)
}

func (s *Server) handlePushSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeMethodNotAllowed(w, r)
		return
	}
	requestID := getRequestID(r)
	if s.pushStore == nil {
		s.writeBusinessError(w, http.StatusServiceUnavailable, 5001, "push subscription store unavailable", requestID, "internal_error", true, nil)
		return
	}
	defer r.Body.Close()

	req, err := decodeWebPushSubscriptionRequest(r)
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{"reason": err.Error()})
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if req.Action != "" && !strings.EqualFold(req.Action, "subscribe") {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid action", requestID, "validation_error", false, map[string]any{"field": "action", "reason": "must be subscribe"})
		return
	}

	rec, err := s.pushStore.Upsert(req)
	if err != nil {
		s.writeBusinessError(w, http.StatusInternalServerError, 5000, "save push subscription failed", requestID, "internal_error", true, map[string]any{"reason": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"id":         rec.ID,
			"endpoint":   rec.Endpoint,
			"created_at": rec.CreatedAt,
			"updated_at": rec.UpdatedAt,
		},
	})
}

func (s *Server) handlePushSubscriptionRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeMethodNotAllowed(w, r)
		return
	}
	requestID := getRequestID(r)
	if s.pushStore == nil {
		s.writeBusinessError(w, http.StatusServiceUnavailable, 5001, "push subscription store unavailable", requestID, "internal_error", true, nil)
		return
	}
	defer r.Body.Close()

	req, err := decodeWebPushSubscriptionRequest(r)
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{"reason": err.Error()})
		return
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(req.Subscription.Endpoint)
	}
	if endpoint == "" {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: endpoint", requestID, "validation_error", false, map[string]any{"field": "endpoint", "reason": "must not be empty"})
		return
	}
	removed, err := s.pushStore.RemoveByEndpoint(endpoint)
	if err != nil {
		s.writeBusinessError(w, http.StatusInternalServerError, 5000, "remove push subscription failed", requestID, "internal_error", true, map[string]any{"reason": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"endpoint": endpoint,
			"removed":  removed,
		},
	})
}

func decodeWebPushSubscriptionRequest(r *http.Request) (webPushSubscriptionRequest, error) {
	var req webPushSubscriptionRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return webPushSubscriptionRequest{}, err
	}
	return req, nil
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
