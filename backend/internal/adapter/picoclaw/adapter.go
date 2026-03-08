package picoclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

const (
	adapterName        = "picoclaw"
	adapterDisplay     = "PicoClaw"
	adapterVersion     = "0.1.0"
	defaultWorkspace   = "/workspace/picoclaw"
	defaultSource      = "pico"
	sessionStatusIdle  = "idle"
	sessionStatusRun   = "running"
	idempotencyTTL     = 5 * time.Minute
	defaultWSBaseURL   = "ws://127.0.0.1:8080"
	defaultDialTimeout = 5 * time.Second
	defaultRunTimeout  = 120 * time.Second
	defaultReadIdle    = 45 * time.Second
)

const (
	picoTypeMessageSend   = "message.send"
	picoTypeMessageCreate = "message.create"
	picoTypeMessageUpdate = "message.update"
	picoTypeTypingStart   = "typing.start"
	picoTypeTypingStop    = "typing.stop"
	picoTypeError         = "error"
	picoTypePong          = "pong"
)

type runtimeOptions struct {
	wsBaseURL       string
	token           string
	allowTokenQuery bool
	dialTimeout     time.Duration
	runTimeout      time.Duration
	readIdleTimeout time.Duration
}

type picoMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type sessionState struct {
	detail      model.SessionDetail
	events      []model.SessionEvent
	nextSeq     int64
	subscribers map[int]*subscriber
	nextSubID   int
}

type subscriber struct {
	ch      chan model.SessionEvent
	ready   bool
	backlog []model.SessionEvent
}

type idempotencyRecord struct {
	job       model.RunJob
	expiresAt time.Time
}

type Adapter struct {
	mu              sync.RWMutex
	sessions        map[string]*sessionState
	deletedSessions map[string]struct{}
	idempotency     map[string]idempotencyRecord

	wsBaseURL       string
	token           string
	allowTokenQuery bool
	dialTimeout     time.Duration
	runTimeout      time.Duration
	readIdleTimeout time.Duration
}

var _ adapter.AgentAdapter = (*Adapter)(nil)

func NewAdapter() (*Adapter, error) {
	opts, err := loadRuntimeOptions()
	if err != nil {
		return nil, err
	}
	if opts.token == "" {
		return nil, fmt.Errorf("PICOCLAW_TOKEN is required")
	}

	return &Adapter{
		sessions:        make(map[string]*sessionState),
		deletedSessions: make(map[string]struct{}),
		idempotency:     make(map[string]idempotencyRecord),
		wsBaseURL:       opts.wsBaseURL,
		token:           opts.token,
		allowTokenQuery: opts.allowTokenQuery,
		dialTimeout:     opts.dialTimeout,
		runTimeout:      opts.runTimeout,
		readIdleTimeout: opts.readIdleTimeout,
	}, nil
}

func (a *Adapter) Name() string { return adapterName }

func (a *Adapter) DisplayName() string { return adapterDisplay }

func (a *Adapter) Version() string { return adapterVersion }

func (a *Adapter) Capabilities() []string {
	return []string{"create_session", "delete_session", "discover_sessions", "events", "continue"}
}

func (a *Adapter) CreateSession(ctx context.Context, req model.CreateSessionInput) (model.SessionDetail, error) {
	select {
	case <-ctx.Done():
		return model.SessionDetail{}, ctx.Err()
	default:
	}

	title := strings.TrimSpace(req.Title)
	workspace := strings.TrimSpace(req.Workspace)
	seedPrompt := strings.TrimSpace(req.SeedPrompt)

	if title == "" {
		title = "New Pico Session"
	}
	if workspace == "" {
		workspace = defaultWorkspace
	}
	if len(title) > 200 {
		return model.SessionDetail{}, fmt.Errorf("%w: title", model.ErrInvalidParam)
	}
	if len(workspace) > 2000 {
		return model.SessionDetail{}, fmt.Errorf("%w: workspace", model.ErrInvalidParam)
	}
	if len(seedPrompt) > 8000 {
		return model.SessionDetail{}, fmt.Errorf("%w: seed_prompt", model.ErrInvalidParam)
	}

	now := time.Now().UTC()
	sessionID := fmt.Sprintf("pico_sess_%d", now.UnixNano())

	s := &sessionState{
		detail: model.SessionDetail{
			Adapter:   adapterName,
			ID:        sessionID,
			Title:     title,
			Status:    sessionStatusIdle,
			CreatedAt: now,
			UpdatedAt: now,
			Workspace: workspace,
			Source:    defaultSource,
			Metadata: map[string]any{
				"channel":     "pico",
				"ws_base_url": a.wsBaseURL,
			},
		},
		events:      make([]model.SessionEvent, 0, 8),
		nextSeq:     1,
		subscribers: make(map[int]*subscriber),
	}

	a.mu.Lock()
	a.sessions[sessionID] = s
	a.mu.Unlock()

	if seedPrompt != "" {
		a.appendUserEvent(sessionID, seedPrompt)
		a.appendAssistantDone(sessionID, "会话已创建，可继续对话。", map[string]any{
			"raw_type": "assistant_done",
			"source":   defaultSource,
		})
	}

	return cloneSessionDetail(s.detail), nil
}

func (a *Adapter) DeleteSession(ctx context.Context, sessionID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.ErrSessionNotFound
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	state, ok := a.sessions[sessionID]
	if !ok {
		return model.ErrSessionNotFound
	}

	for subID, sub := range state.subscribers {
		delete(state.subscribers, subID)
		close(sub.ch)
	}

	delete(a.sessions, sessionID)
	a.deletedSessions[sessionID] = struct{}{}
	for key := range a.idempotency {
		if strings.HasPrefix(key, sessionID+"::") {
			delete(a.idempotency, key)
		}
	}
	return nil
}

func (a *Adapter) DiscoverSessions(ctx context.Context, req model.DiscoverRequest) (model.PagedSessions, error) {
	if req.Limit <= 0 {
		req.Limit = model.DefaultSessionsLimit
	}
	if req.Limit > model.MaxSessionsLimit {
		return model.PagedSessions{}, fmt.Errorf("%w: limit", model.ErrInvalidParam)
	}
	if req.SortBy == "" {
		req.SortBy = "updated_at"
	}
	if req.SortBy != "updated_at" {
		return model.PagedSessions{}, fmt.Errorf("%w: sort_by", model.ErrInvalidParam)
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}
	if req.SortOrder != "asc" && req.SortOrder != "desc" {
		return model.PagedSessions{}, fmt.Errorf("%w: sort_order", model.ErrInvalidParam)
	}
	offset, err := model.DecodeIndexCursor(req.Cursor)
	if err != nil {
		return model.PagedSessions{}, fmt.Errorf("%w: cursor", model.ErrInvalidParam)
	}

	query := strings.ToLower(strings.TrimSpace(req.Query))
	items := make([]model.SessionSummary, 0, len(a.sessions))
	a.mu.RLock()
	for _, state := range a.sessions {
		select {
		case <-ctx.Done():
			a.mu.RUnlock()
			return model.PagedSessions{}, ctx.Err()
		default:
		}

		item := summarizeSession(state.detail)
		if query != "" && !strings.Contains(strings.ToLower(item.Title), query) {
			continue
		}
		if req.Workspace != "" && req.Workspace != item.Workspace {
			continue
		}
		if req.UpdatedAfter != nil && item.UpdatedAt.Before(*req.UpdatedAfter) {
			continue
		}
		if req.UpdatedBefore != nil && item.UpdatedAt.After(*req.UpdatedBefore) {
			continue
		}
		items = append(items, item)
	}
	a.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		if req.SortOrder == "asc" {
			return items[i].UpdatedAt.Before(items[j].UpdatedAt)
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})

	if offset > len(items) {
		return model.PagedSessions{}, fmt.Errorf("%w: cursor", model.ErrInvalidParam)
	}
	end := offset + req.Limit
	if end > len(items) {
		end = len(items)
	}

	page := items[offset:end]
	hasMore := end < len(items)
	nextCursor := ""
	if hasMore {
		nextCursor = model.EncodeIndexCursor(end)
	}

	return model.PagedSessions{Items: page, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (a *Adapter) GetSession(ctx context.Context, sessionID string) (model.SessionDetail, error) {
	select {
	case <-ctx.Done():
		return model.SessionDetail{}, ctx.Err()
	default:
	}

	a.mu.RLock()
	state, ok := a.sessions[strings.TrimSpace(sessionID)]
	a.mu.RUnlock()
	if !ok {
		return model.SessionDetail{}, model.ErrSessionNotFound
	}
	return cloneSessionDetail(state.detail), nil
}

func (a *Adapter) GetSessionEvents(ctx context.Context, req model.EventsRequest) (model.PagedEvents, error) {
	if req.Limit <= 0 {
		req.Limit = model.DefaultEventsLimit
	}
	if req.Limit > model.MaxEventsLimit {
		return model.PagedEvents{}, fmt.Errorf("%w: limit", model.ErrInvalidParam)
	}
	sinceSeq, err := model.DecodeSeqCursor(req.Cursor)
	if err != nil {
		return model.PagedEvents{}, fmt.Errorf("%w: cursor", model.ErrInvalidParam)
	}

	a.mu.RLock()
	state, ok := a.sessions[strings.TrimSpace(req.SessionID)]
	if !ok {
		a.mu.RUnlock()
		return model.PagedEvents{}, model.ErrSessionNotFound
	}

	filtered := make([]model.SessionEvent, 0, len(state.events))
	for _, ev := range state.events {
		if ev.Seq > sinceSeq {
			filtered = append(filtered, ev)
		}
	}
	a.mu.RUnlock()

	end := req.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	items := filtered[:end]
	hasMore := end < len(filtered)
	nextCursor := ""
	if len(items) > 0 {
		nextCursor = model.EncodeSeqCursor(items[len(items)-1].Seq)
	}

	return model.PagedEvents{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (a *Adapter) ContinueSession(ctx context.Context, req model.ContinueInput) (model.RunJob, error) {
	if err := ctx.Err(); err != nil {
		return model.RunJob{}, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if len(prompt) == 0 || len(prompt) > 8000 {
		return model.RunJob{}, fmt.Errorf("%w: prompt", model.ErrInvalidParam)
	}

	sessionID := strings.TrimSpace(req.SessionID)
	a.mu.Lock()
	state, ok := a.sessions[sessionID]
	if !ok {
		a.mu.Unlock()
		return model.RunJob{}, model.ErrSessionNotFound
	}

	now := time.Now().UTC()
	for key, record := range a.idempotency {
		if !record.expiresAt.After(now) {
			delete(a.idempotency, key)
		}
	}
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		cacheKey := sessionID + "::" + key
		if record, exists := a.idempotency[cacheKey]; exists && record.expiresAt.After(now) {
			a.mu.Unlock()
			return record.job, nil
		}
	}

	state.detail.Status = sessionStatusRun
	state.detail.UpdatedAt = now
	job := model.RunJob{
		JobID:     fmt.Sprintf("job_%d", now.UnixNano()),
		Adapter:   adapterName,
		SessionID: sessionID,
		Status:    "accepted",
		StartedAt: now,
	}
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		a.idempotency[sessionID+"::"+key] = idempotencyRecord{
			job:       job,
			expiresAt: now.Add(idempotencyTTL),
		}
	}
	a.mu.Unlock()

	go func() {
		defer a.setSessionStatus(sessionID, sessionStatusIdle)
		a.emitContinueEvents(ctx, sessionID, prompt)
	}()

	return job, nil
}

func (a *Adapter) Subscribe(ctx context.Context, sessionID string, fromSeq int64) (<-chan model.SessionEvent, func(), error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, model.ErrSessionNotFound
	}

	a.mu.Lock()
	state, ok := a.sessions[sessionID]
	if !ok {
		a.mu.Unlock()
		return nil, nil, model.ErrSessionNotFound
	}

	subID := state.nextSubID
	state.nextSubID++
	sub := &subscriber{
		ch:      make(chan model.SessionEvent, 64),
		ready:   false,
		backlog: make([]model.SessionEvent, 0, 16),
	}
	state.subscribers[subID] = sub

	history := make([]model.SessionEvent, 0, len(state.events))
	for _, ev := range state.events {
		if ev.Seq > fromSeq {
			history = append(history, ev)
		}
	}
	a.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			a.mu.Lock()
			if s, exists := a.sessions[sessionID]; exists {
				if current, found := s.subscribers[subID]; found {
					delete(s.subscribers, subID)
					close(current.ch)
				}
			}
			a.mu.Unlock()
		})
	}

	for _, ev := range history {
		if !sendEventWithContext(ctx, sub.ch, ev) {
			unsubscribe()
			return nil, nil, context.Canceled
		}
	}

	for {
		a.mu.Lock()
		s, exists := a.sessions[sessionID]
		if !exists {
			a.mu.Unlock()
			unsubscribe()
			return nil, nil, model.ErrSessionNotFound
		}

		current, found := s.subscribers[subID]
		if !found {
			a.mu.Unlock()
			return nil, nil, context.Canceled
		}
		if len(current.backlog) == 0 {
			current.ready = true
			a.mu.Unlock()
			break
		}

		backlog := append([]model.SessionEvent(nil), current.backlog...)
		current.backlog = current.backlog[:0]
		a.mu.Unlock()

		for _, ev := range backlog {
			if !sendEventWithContext(ctx, current.ch, ev) {
				unsubscribe()
				return nil, nil, context.Canceled
			}
		}
	}

	return sub.ch, unsubscribe, nil
}

func (a *Adapter) HealthCheck(ctx context.Context) (int64, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	testSessionID := fmt.Sprintf("hc_%d", start.UnixNano())
	conn, err := a.dialSessionWS(ctx, testSessionID)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start).Milliseconds(), nil
}

func (a *Adapter) emitContinueEvents(ctx context.Context, sessionID, prompt string) {
	a.appendUserEvent(sessionID, prompt)
	if err := a.streamPicoSession(ctx, sessionID, prompt); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		a.appendAssistantFailure(sessionID, err)
	}
}

func (a *Adapter) streamPicoSession(ctx context.Context, sessionID, prompt string) error {
	runCtx := ctx
	cancel := func() {}
	if a.runTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, a.runTimeout)
	}
	defer cancel()

	conn, err := a.dialSessionWS(runCtx, sessionID)
	if err != nil {
		return err
	}
	defer conn.Close()

	conn.SetReadLimit(8 << 20)
	if a.readIdleTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(a.readIdleTimeout))
		conn.SetPongHandler(func(appData string) error {
			_ = conn.SetReadDeadline(time.Now().Add(a.readIdleTimeout))
			return nil
		})
		conn.SetPingHandler(func(appData string) error {
			_ = conn.SetReadDeadline(time.Now().Add(a.readIdleTimeout))
			deadline := time.Now().Add(5 * time.Second)
			return conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
		})
	}

	req := picoMessage{
		Type:      picoTypeMessageSend,
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"content": prompt,
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		return fmt.Errorf("write pico message.send failed: %w", err)
	}

	messageCache := make(map[string]string)
	doneSent := false

	for {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if a.readIdleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(a.readIdleTimeout))
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return fmt.Errorf("pico stream read timeout: %w", err)
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				if !doneSent {
					a.appendAssistantDone(sessionID, "", map[string]any{
						"raw_type": "stream_closed",
						"source":   defaultSource,
					})
				}
				return nil
			}
			return fmt.Errorf("read pico stream failed: %w", err)
		}

		var msg picoMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case picoTypeTypingStart:
			a.appendAssistantAction(sessionID, "正在生成回复", map[string]any{
				"raw_type": picoTypeTypingStart,
				"source":   defaultSource,
			})
		case picoTypeMessageCreate, picoTypeMessageUpdate:
			text := extractPicoText(msg.Payload)
			if text == "" {
				continue
			}
			msgID := extractPicoMessageID(msg)
			delta := text
			if msgID != "" {
				prev := messageCache[msgID]
				if prev != "" && strings.HasPrefix(text, prev) {
					delta = strings.TrimPrefix(text, prev)
				}
				messageCache[msgID] = text
			}
			if delta == "" {
				continue
			}
			a.appendAssistantDelta(sessionID, delta, map[string]any{
				"raw_type":   msg.Type,
				"source":     defaultSource,
				"message_id": msgID,
			})
		case picoTypeTypingStop:
			if !doneSent {
				a.appendAssistantDone(sessionID, "", map[string]any{
					"raw_type": picoTypeTypingStop,
					"source":   defaultSource,
				})
				doneSent = true
			}
			return nil
		case picoTypeError:
			return fmt.Errorf("pico error: %s", extractPicoError(msg.Payload))
		case picoTypePong:
			continue
		default:
			continue
		}
	}
}

func (a *Adapter) dialSessionWS(ctx context.Context, sessionID string) (*websocket.Conn, error) {
	u, err := buildSessionWSURL(a.wsBaseURL, sessionID, a.token, a.allowTokenQuery)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if a.token != "" {
		headers.Set("Authorization", "Bearer "+a.token)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: a.dialTimeout,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, resp, err := dialer.DialContext(ctx, u, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial pico ws failed: status=%d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("dial pico ws failed: %w", err)
	}
	return conn, nil
}

func buildSessionWSURL(base, sessionID, token string, allowTokenQuery bool) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid ws base url: %w", err)
	}

	path := strings.TrimSuffix(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/pico/ws"):
		u.Path = path
	case strings.HasSuffix(path, "/pico"):
		u.Path = path + "/ws"
	case path == "" || path == "/":
		u.Path = "/pico/ws"
	default:
		u.Path = path + "/pico/ws"
	}

	q := u.Query()
	q.Set("session_id", sessionID)
	if allowTokenQuery && token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func summarizeSession(detail model.SessionDetail) model.SessionSummary {
	return model.SessionSummary{
		Adapter:   detail.Adapter,
		ID:        detail.ID,
		Title:     detail.Title,
		Status:    normalizeSessionStatus(detail.Status),
		UpdatedAt: detail.UpdatedAt,
		Workspace: detail.Workspace,
		Source:    detail.Source,
	}
}

func cloneSessionDetail(detail model.SessionDetail) model.SessionDetail {
	clone := detail
	clone.Status = normalizeSessionStatus(clone.Status)
	if detail.Metadata == nil {
		return clone
	}
	clone.Metadata = make(map[string]any, len(detail.Metadata))
	for k, v := range detail.Metadata {
		clone.Metadata[k] = v
	}
	return clone
}

func normalizeSessionStatus(status string) string {
	if strings.EqualFold(strings.TrimSpace(status), sessionStatusRun) {
		return sessionStatusRun
	}
	return sessionStatusIdle
}

func (a *Adapter) appendUserEvent(sessionID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.appendEvent(sessionID, "message.user", map[string]any{
		"raw_type": "user_message",
		"source":   defaultSource,
		"text":     text,
	}, map[string]any{
		"role": "user",
		"text": text,
		"done": true,
	})
}

func (a *Adapter) appendAssistantDelta(sessionID, text string, payload map[string]any) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["text"] = text
	a.appendEvent(sessionID, "message.delta", payload, map[string]any{
		"role": "assistant",
		"text": text,
		"done": false,
	})
}

func (a *Adapter) appendAssistantDone(sessionID, text string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["text"] = text
	a.appendEvent(sessionID, "message.done", payload, map[string]any{
		"role": "assistant",
		"text": text,
		"done": true,
	})
}

func (a *Adapter) appendAssistantAction(sessionID, action string, payload map[string]any) {
	action = strings.TrimSpace(action)
	if action == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["action"] = action
	a.appendEvent(sessionID, "message.action", payload, map[string]any{
		"role":   "assistant",
		"text":   "",
		"done":   false,
		"action": action,
	})
}

func (a *Adapter) appendAssistantFailure(sessionID string, err error) {
	msg := "continue 执行失败"
	if err != nil {
		msg += ": " + err.Error()
	}
	a.appendAssistantDone(sessionID, msg, map[string]any{
		"raw_type": "assistant_error",
		"source":   defaultSource,
		"error":    msg,
	})
}

func (a *Adapter) appendEvent(sessionID, eventType string, payload, normalized map[string]any) {
	now := time.Now().UTC()
	a.mu.Lock()
	state, ok := a.sessions[sessionID]
	if !ok {
		a.mu.Unlock()
		return
	}

	ev := model.SessionEvent{
		Adapter:    adapterName,
		SessionID:  sessionID,
		Seq:        state.nextSeq,
		Ts:         now,
		Type:       eventType,
		Payload:    payload,
		Normalized: normalized,
	}
	state.nextSeq++
	state.events = append(state.events, ev)
	state.detail.UpdatedAt = ev.Ts

	readySubs := make([]*subscriber, 0, len(state.subscribers))
	for _, sub := range state.subscribers {
		if sub.ready {
			readySubs = append(readySubs, sub)
			continue
		}
		sub.backlog = append(sub.backlog, ev)
	}
	a.mu.Unlock()

	for _, sub := range readySubs {
		sendEvent(sub.ch, ev)
	}
}

func (a *Adapter) setSessionStatus(sessionID, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.sessions[sessionID]
	if !ok {
		return
	}
	state.detail.Status = normalizeSessionStatus(status)
	state.detail.UpdatedAt = time.Now().UTC()
}

func extractPicoText(payload map[string]any) string {
	if payload == nil {
		return ""
	}

	if text, ok := payload["content"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := payload["text"].(string); ok {
		return strings.TrimSpace(text)
	}

	if node, ok := payload["content"].(map[string]any); ok {
		if text, ok := node["text"].(string); ok {
			return strings.TrimSpace(text)
		}
		if text, ok := node["content"].(string); ok {
			return strings.TrimSpace(text)
		}
	}

	if parts, ok := payload["content"].([]any); ok {
		segments := make([]string, 0, len(parts))
		for _, part := range parts {
			switch p := part.(type) {
			case string:
				if s := strings.TrimSpace(p); s != "" {
					segments = append(segments, s)
				}
			case map[string]any:
				if text, ok := p["text"].(string); ok {
					if s := strings.TrimSpace(text); s != "" {
						segments = append(segments, s)
					}
					continue
				}
				if text, ok := p["content"].(string); ok {
					if s := strings.TrimSpace(text); s != "" {
						segments = append(segments, s)
					}
				}
			}
		}
		return strings.Join(segments, "\n")
	}

	return ""
}

func extractPicoMessageID(msg picoMessage) string {
	if msg.Payload != nil {
		if v, ok := msg.Payload["message_id"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
		if v, ok := msg.Payload["id"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(msg.ID)
}

func extractPicoError(payload map[string]any) string {
	if payload == nil {
		return "unknown error"
	}
	code, _ := payload["code"].(string)
	message, _ := payload["message"].(string)
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	switch {
	case code != "" && message != "":
		return code + ": " + message
	case message != "":
		return message
	case code != "":
		return code
	default:
		return "unknown error"
	}
}

func sendEvent(ch chan model.SessionEvent, ev model.SessionEvent) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ch <- ev
	return true
}

func sendEventWithContext(ctx context.Context, ch chan model.SessionEvent, ev model.SessionEvent) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}

func loadRuntimeOptions() (runtimeOptions, error) {
	opts := runtimeOptions{
		dialTimeout:     defaultDialTimeout,
		runTimeout:      defaultRunTimeout,
		readIdleTimeout: defaultReadIdle,
		allowTokenQuery: parseBoolEnv("PICOCLAW_ALLOW_TOKEN_QUERY", false),
	}

	rawBase := strings.TrimSpace(os.Getenv("PICOCLAW_WS_BASE_URL"))
	if rawBase == "" {
		rawBase = defaultWSBaseURL
	}
	base, err := normalizeWSBaseURL(rawBase)
	if err != nil {
		return runtimeOptions{}, err
	}
	opts.wsBaseURL = base
	opts.token = strings.TrimSpace(os.Getenv("PICOCLAW_TOKEN"))

	if ms := parsePositiveMS("PICOCLAW_DIAL_TIMEOUT_MS"); ms > 0 {
		opts.dialTimeout = ms
	}
	if ms := parsePositiveMS("PICOCLAW_CONTINUE_TIMEOUT_MS"); ms > 0 {
		opts.runTimeout = ms
	}
	if ms := parsePositiveMS("PICOCLAW_READ_IDLE_TIMEOUT_MS"); ms > 0 {
		opts.readIdleTimeout = ms
	}

	return opts, nil
}

func normalizeWSBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty ws base url")
	}
	if !strings.Contains(raw, "://") {
		raw = "ws://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid ws base url: %w", err)
	}

	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	switch scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// keep as-is
	default:
		return "", fmt.Errorf("unsupported ws base scheme %q", u.Scheme)
	}

	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("invalid ws base url: host is required")
	}

	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

func parsePositiveMS(key string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func parseBoolEnv(key string, fallback bool) bool {
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
