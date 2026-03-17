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
	"path/filepath"
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
	defaultHistoryDir  = "~/.picoclaw/workspace/sessions"
	defaultDialTimeout = 5 * time.Second
	defaultRunTimeout  = 120 * time.Second
	defaultReadIdle    = 45 * time.Second
	defaultMaxWatchers = 64
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
	historyEnabled  bool
	historyDir      string
	maxLiveWatchers int
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
	activeRuns  int
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
	liveWatchers    map[string]context.CancelFunc
	watchCtx        context.Context
	watchCancel     context.CancelFunc

	wsBaseURL       string
	token           string
	allowTokenQuery bool
	dialTimeout     time.Duration
	runTimeout      time.Duration
	readIdleTimeout time.Duration
	historyEnabled  bool
	historyDir      string
	maxLiveWatchers int
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

	watchCtx, watchCancel := context.WithCancel(context.Background())
	return &Adapter{
		sessions:        make(map[string]*sessionState),
		deletedSessions: make(map[string]struct{}),
		idempotency:     make(map[string]idempotencyRecord),
		liveWatchers:    make(map[string]context.CancelFunc),
		watchCtx:        watchCtx,
		watchCancel:     watchCancel,
		wsBaseURL:       opts.wsBaseURL,
		token:           opts.token,
		allowTokenQuery: opts.allowTokenQuery,
		dialTimeout:     opts.dialTimeout,
		runTimeout:      opts.runTimeout,
		readIdleTimeout: opts.readIdleTimeout,
		historyEnabled:  opts.historyEnabled,
		historyDir:      opts.historyDir,
		maxLiveWatchers: opts.maxLiveWatchers,
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
	a.stopLiveWatcher(sessionID)

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
	a.syncHistorySessions()

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
	a.ensureLiveWatchersForSessions(page)

	return model.PagedSessions{Items: page, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (a *Adapter) GetSession(ctx context.Context, sessionID string) (model.SessionDetail, error) {
	select {
	case <-ctx.Done():
		return model.SessionDetail{}, ctx.Err()
	default:
	}
	a.syncHistorySessions()
	sessionID = strings.TrimSpace(sessionID)
	a.ensureLiveWatcher(sessionID)

	a.mu.RLock()
	state, ok := a.sessions[sessionID]
	a.mu.RUnlock()
	if !ok {
		return model.SessionDetail{}, model.ErrSessionNotFound
	}
	return cloneSessionDetail(state.detail), nil
}

func (a *Adapter) GetSessionEvents(ctx context.Context, req model.EventsRequest) (model.PagedEvents, error) {
	a.syncHistorySessions()
	req.SessionID = strings.TrimSpace(req.SessionID)
	a.ensureLiveWatcher(req.SessionID)

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
	state, ok := a.sessions[req.SessionID]
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
	a.syncHistorySessions()

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
	state.activeRuns++
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
	a.ensureLiveWatcher(sessionID)

	go func() {
		defer a.finishSessionRun(sessionID)
		a.emitContinueEvents(ctx, sessionID, prompt)
	}()

	return job, nil
}

func (a *Adapter) Subscribe(ctx context.Context, sessionID string, fromSeq int64) (<-chan model.SessionEvent, func(), error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, model.ErrSessionNotFound
	}
	// Bridge mode: when ASWG WS subscribes, ensure we have an upstream pico WS
	// watcher so passive/out-of-band pico pushes can flow through to subscribers.
	a.ensureLiveWatcher(sessionID)

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
	assistantSeen := false
	typingStopped := false
	var typingStopDeadline time.Time

	for {
		if err := runCtx.Err(); err != nil {
			return err
		}
		readWait := a.readIdleTimeout
		if typingStopped {
			remaining := time.Until(typingStopDeadline)
			if remaining <= 0 {
				if !doneSent {
					a.appendAssistantDone(sessionID, "", map[string]any{
						"raw_type": "typing.stop",
						"source":   defaultSource,
					})
					doneSent = true
				}
				return nil
			}
			if readWait <= 0 || remaining < readWait {
				readWait = remaining
			}
		}
		if readWait > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(readWait))
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if typingStopped {
					if !doneSent {
						a.appendAssistantDone(sessionID, "", map[string]any{
							"raw_type": "typing.stop",
							"source":   defaultSource,
						})
						doneSent = true
					}
					return nil
				}
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
		if !messageBelongsToSession(msg, sessionID) {
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
			assistantSeen = true
			if typingStopped && !doneSent {
				a.appendAssistantDone(sessionID, "", map[string]any{
					"raw_type": "typing.stop",
					"source":   defaultSource,
				})
				doneSent = true
				return nil
			}
		case picoTypeTypingStop:
			typingStopped = true
			typingStopDeadline = time.Now().Add(2 * time.Second)
			if assistantSeen && !doneSent {
				a.appendAssistantDone(sessionID, "", map[string]any{
					"raw_type": picoTypeTypingStop,
					"source":   defaultSource,
				})
				doneSent = true
				return nil
			}
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

func (a *Adapter) ensureLiveWatcher(sessionID string) {
	a.ensureLiveWatcherInternal(sessionID, true)
}

func (a *Adapter) ensureLiveWatcherBestEffort(sessionID string) {
	a.ensureLiveWatcherInternal(sessionID, false)
}

func (a *Adapter) ensureLiveWatcherInternal(sessionID string, force bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	a.mu.Lock()
	if !force && a.maxLiveWatchers > 0 && len(a.liveWatchers) >= a.maxLiveWatchers {
		if _, exists := a.liveWatchers[sessionID]; !exists {
			a.mu.Unlock()
			return
		}
	}
	if _, ok := a.sessions[sessionID]; !ok {
		a.mu.Unlock()
		return
	}
	if _, exists := a.liveWatchers[sessionID]; exists {
		a.mu.Unlock()
		return
	}
	parent := a.watchCtx
	if parent == nil {
		parent = context.Background()
	}
	watchCtx, cancel := context.WithCancel(parent)
	a.liveWatchers[sessionID] = cancel
	a.mu.Unlock()

	go a.liveWatchLoop(watchCtx, sessionID)
}

func (a *Adapter) ensureLiveWatchersForSessions(items []model.SessionSummary) {
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		a.ensureLiveWatcherBestEffort(item.ID)
	}
}

func (a *Adapter) stopLiveWatcher(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	var cancel context.CancelFunc
	a.mu.Lock()
	if fn, ok := a.liveWatchers[sessionID]; ok {
		cancel = fn
		delete(a.liveWatchers, sessionID)
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Adapter) liveWatchLoop(ctx context.Context, sessionID string) {
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		conn, err := a.dialSessionWS(ctx, sessionID)
		if err != nil {
			if !sleepWithContext(ctx, backoff) {
				return
			}
			if backoff < 15*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		a.liveWatchReadLoop(ctx, sessionID, conn)
		_ = conn.Close()
	}
}

func (a *Adapter) liveWatchReadLoop(ctx context.Context, sessionID string, conn *websocket.Conn) {
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

	typing := false
	messageCache := make(map[string]string)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if a.readIdleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(a.readIdleTimeout))
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return
			}
			return
		}

		var msg picoMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if !messageBelongsToSession(msg, sessionID) {
			continue
		}
		a.handleLiveMessage(sessionID, &typing, messageCache, msg)
	}
}

func (a *Adapter) handleLiveMessage(sessionID string, typing *bool, messageCache map[string]string, msg picoMessage) {
	if a.sessionHasActiveRun(sessionID) {
		return
	}

	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case picoTypeTypingStart:
		a.appendAssistantAction(sessionID, "正在生成回复", map[string]any{
			"raw_type": msg.Type,
			"source":   defaultSource,
			"origin":   "live_watch",
		})
		*typing = true
	case picoTypeMessageCreate, picoTypeMessageUpdate:
		text := extractPicoText(msg.Payload)
		if text == "" {
			return
		}
		msgID := extractPicoMessageID(msg)
		if *typing {
			delta := text
			if msgID != "" {
				prev := messageCache[msgID]
				if prev != "" && strings.HasPrefix(text, prev) {
					delta = strings.TrimPrefix(text, prev)
				}
				messageCache[msgID] = text
			}
			if delta == "" {
				return
			}
			a.appendAssistantDelta(sessionID, delta, map[string]any{
				"raw_type":   msg.Type,
				"source":     defaultSource,
				"message_id": msgID,
				"origin":     "live_watch",
			})
			return
		}
		if msgID != "" {
			if prev, ok := messageCache[msgID]; ok && prev == text {
				return
			}
			messageCache[msgID] = text
		}
		a.appendAssistantDone(sessionID, text, map[string]any{
			"raw_type":   msg.Type,
			"source":     defaultSource,
			"message_id": msgID,
			"origin":     "live_watch",
		})
	case picoTypeTypingStop:
		if *typing {
			a.appendAssistantDone(sessionID, "", map[string]any{
				"raw_type": msg.Type,
				"source":   defaultSource,
				"origin":   "live_watch",
			})
		}
		*typing = false
	case picoTypePong:
		return
	default:
		return
	}
}

func (a *Adapter) sessionHasActiveRun(sessionID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state, ok := a.sessions[sessionID]
	if !ok {
		return false
	}
	return state.activeRuns > 0
}

func (a *Adapter) finishSessionRun(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.sessions[sessionID]
	if !ok {
		return
	}
	if state.activeRuns > 0 {
		state.activeRuns--
	}
	if state.activeRuns == 0 {
		state.detail.Status = sessionStatusIdle
		state.detail.UpdatedAt = time.Now().UTC()
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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

func messageBelongsToSession(msg picoMessage, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return true
	}
	candidate := strings.TrimSpace(msg.SessionID)
	if candidate == "" {
		candidate = extractSessionIDFromPayload(msg.Payload)
	}
	if candidate == "" {
		// Keep backward compatibility with upstream payloads that don't
		// carry explicit session identifiers.
		return true
	}
	return candidate == sessionID
}

func extractSessionIDFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["session_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := payload["sessionId"].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := payload["chat_id"].(string); ok {
		return parseSessionIDFromChatID(v)
	}
	if v, ok := payload["chatId"].(string); ok {
		return parseSessionIDFromChatID(v)
	}
	return ""
}

func parseSessionIDFromChatID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ":")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if strings.HasPrefix(part, "pico_sess_") || strings.HasPrefix(part, "hc_") {
			return part
		}
	}
	return ""
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

type historySessionFile struct {
	Key      string `json:"key"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

type historySnapshot struct {
	id        string
	title     string
	createdAt time.Time
	updatedAt time.Time
	events    []model.SessionEvent
	filePath  string
}

func (a *Adapter) syncHistorySessions() {
	if !a.historyEnabled {
		return
	}
	historyDir := strings.TrimSpace(a.historyDir)
	if historyDir == "" {
		return
	}

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		return
	}

	snapshots := make(map[string]historySnapshot, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		path := filepath.Join(historyDir, name)
		snap, ok := parseHistorySnapshot(path)
		if !ok {
			continue
		}
		if prev, exists := snapshots[snap.id]; exists && !snap.updatedAt.After(prev.updatedAt) {
			continue
		}
		snapshots[snap.id] = snap
	}
	if len(snapshots) == 0 {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for sessionID, snap := range snapshots {
		if _, deleted := a.deletedSessions[sessionID]; deleted {
			continue
		}
		if state, ok := a.sessions[sessionID]; ok {
			imported := isHistoryImported(state.detail.Metadata)
			if imported && normalizeSessionStatus(state.detail.Status) != sessionStatusRun {
				if snap.updatedAt.After(state.detail.UpdatedAt) || len(snap.events) > len(state.events) {
					state.events = cloneSessionEvents(snap.events)
					state.nextSeq = int64(len(state.events) + 1)
					state.detail.Status = sessionStatusIdle
					state.detail.UpdatedAt = snap.updatedAt
					if !snap.createdAt.IsZero() {
						state.detail.CreatedAt = snap.createdAt
					}
					if strings.TrimSpace(snap.title) != "" {
						state.detail.Title = snap.title
					}
					if state.detail.Metadata == nil {
						state.detail.Metadata = map[string]any{}
					}
					state.detail.Metadata["history_file"] = snap.filePath
					state.detail.Metadata["history_imported"] = true
				}
			}
			if strings.TrimSpace(state.detail.Title) == "" {
				state.detail.Title = snap.title
			}
			if state.detail.UpdatedAt.Before(snap.updatedAt) {
				state.detail.UpdatedAt = snap.updatedAt
			}
			continue
		}

		createdAt := snap.createdAt
		if createdAt.IsZero() {
			createdAt = snap.updatedAt
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		updatedAt := snap.updatedAt
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		title := strings.TrimSpace(snap.title)
		if title == "" {
			title = sessionID
		}

		a.sessions[sessionID] = &sessionState{
			detail: model.SessionDetail{
				Adapter:   adapterName,
				ID:        sessionID,
				Title:     title,
				Status:    sessionStatusIdle,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
				Workspace: defaultWorkspace,
				Source:    defaultSource,
				Metadata: map[string]any{
					"channel":          "pico",
					"ws_base_url":      a.wsBaseURL,
					"history_file":     snap.filePath,
					"history_imported": true,
				},
			},
			events:      cloneSessionEvents(snap.events),
			nextSeq:     int64(len(snap.events) + 1),
			subscribers: make(map[int]*subscriber),
		}
	}
}

func parseHistorySnapshot(path string) (historySnapshot, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return historySnapshot{}, false
	}

	var src historySessionFile
	if err := json.Unmarshal(content, &src); err != nil {
		return historySnapshot{}, false
	}

	sessionID := extractSessionIDFromHistoryKey(src.Key)
	if sessionID == "" {
		return historySnapshot{}, false
	}

	createdAt := parseSessionTime(src.Created)
	updatedAt := parseSessionTime(src.Updated)
	if updatedAt.IsZero() {
		if info, statErr := os.Stat(path); statErr == nil {
			updatedAt = info.ModTime().UTC()
		}
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	events := buildHistoryEvents(sessionID, src.Messages, createdAt, updatedAt)
	title := deriveHistoryTitle(src.Messages, sessionID)

	return historySnapshot{
		id:        sessionID,
		title:     title,
		createdAt: createdAt,
		updatedAt: updatedAt,
		events:    events,
		filePath:  path,
	}, true
}

func extractSessionIDFromHistoryKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	const marker = ":pico:direct:pico:"
	idx := strings.LastIndex(key, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(key[idx+len(marker):])
}

func parseSessionTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func deriveHistoryTitle(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, fallback string) string {
	for _, msg := range messages {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		text := strings.Join(strings.Fields(msg.Content), " ")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 120 {
			return string(runes[:117]) + "..."
		}
		return text
	}
	return strings.TrimSpace(fallback)
}

func buildHistoryEvents(sessionID string, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, createdAt, updatedAt time.Time) []model.SessionEvent {
	type historyMessage struct {
		role string
		text string
	}
	filtered := make([]historyMessage, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		filtered = append(filtered, historyMessage{role: role, text: text})
	}
	if len(filtered) == 0 {
		return nil
	}

	base := createdAt
	if base.IsZero() {
		base = updatedAt
	}
	if base.IsZero() {
		base = time.Now().UTC()
	}
	step := 50 * time.Millisecond
	if updatedAt.After(base) {
		if candidate := updatedAt.Sub(base) / time.Duration(len(filtered)); candidate > 0 {
			step = candidate
		}
	}

	events := make([]model.SessionEvent, 0, len(filtered))
	seq := int64(1)
	for i, msg := range filtered {
		ts := base.Add(step * time.Duration(i))
		if i == len(filtered)-1 && updatedAt.After(ts) {
			ts = updatedAt
		}
		switch msg.role {
		case "user":
			events = append(events, model.SessionEvent{
				Adapter:   adapterName,
				SessionID: sessionID,
				Seq:       seq,
				Ts:        ts,
				Type:      "message.user",
				Payload: map[string]any{
					"raw_type": "history_message",
					"source":   defaultSource,
					"text":     msg.text,
				},
				Normalized: map[string]any{
					"role": "user",
					"text": msg.text,
					"done": true,
				},
			})
		case "assistant":
			events = append(events, model.SessionEvent{
				Adapter:   adapterName,
				SessionID: sessionID,
				Seq:       seq,
				Ts:        ts,
				Type:      "message.done",
				Payload: map[string]any{
					"raw_type": "history_message",
					"source":   defaultSource,
					"text":     msg.text,
				},
				Normalized: map[string]any{
					"role": "assistant",
					"text": msg.text,
					"done": true,
				},
			})
		}
		seq++
	}
	return events
}

func isHistoryImported(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	value, ok := meta["history_imported"]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		v = strings.TrimSpace(strings.ToLower(v))
		return v == "1" || v == "true" || v == "yes"
	default:
		return false
	}
}

func cloneSessionEvents(items []model.SessionEvent) []model.SessionEvent {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.SessionEvent, len(items))
	copy(out, items)
	return out
}

func loadRuntimeOptions() (runtimeOptions, error) {
	opts := runtimeOptions{
		dialTimeout:     defaultDialTimeout,
		runTimeout:      defaultRunTimeout,
		readIdleTimeout: defaultReadIdle,
		allowTokenQuery: parseBoolEnv("PICOCLAW_ALLOW_TOKEN_QUERY", false),
		historyEnabled:  parseBoolEnv("PICOCLAW_HISTORY_ENABLED", true),
		maxLiveWatchers: defaultMaxWatchers,
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
	rawHistoryDir := strings.TrimSpace(os.Getenv("PICOCLAW_HISTORY_DIR"))
	if rawHistoryDir == "" {
		rawHistoryDir = defaultHistoryDir
	}
	opts.historyDir = expandHomePath(rawHistoryDir)
	if n := parsePositiveInt("PICOCLAW_MAX_LIVE_WATCHERS"); n > 0 {
		opts.maxLiveWatchers = n
	}

	return opts, nil
}

func expandHomePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "~") {
		return raw
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return raw
	}
	if raw == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(raw, "~/"))
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

func parsePositiveInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0
	}
	return v
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
