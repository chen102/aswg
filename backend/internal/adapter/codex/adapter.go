package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

const (
	adapterName          = "codex"
	adapterDisplay       = "Codex"
	adapterVersion       = "0.1.0"
	defaultSessionID     = "sess_demo_001"
	defaultWorkspace     = "/workspace/demo"
	defaultSource        = "cli"
	defaultNewWorkspace  = "/workspace/new"
	continueChunkWait    = 250 * time.Millisecond
	idempotencyTTL       = 5 * time.Minute
	defaultCLIBin        = "codex"
	defaultCLIArgs       = "exec --json --dangerously-bypass-approvals-and-sandbox"
	cliBypassFlag        = "--dangerously-bypass-approvals-and-sandbox"
	defaultCLITimeout    = 30 * time.Minute
	defaultHistoryTTL    = 5 * time.Second
	defaultHistoryWindow = 30 * time.Minute
	defaultHistoryTail   = 2 * time.Second
	defaultHistorySource = "history"
	sessionStatusIdle    = "idle"
	sessionStatusRunning = "running"
)

type sessionState struct {
	detail              model.SessionDetail
	events              []model.SessionEvent
	nextSeq             int64
	subscribers         map[int]*subscriber
	nextSubID           int
	codexThread         string
	activeRuns          int
	historyPath         string
	historyOffset       int64
	historyFollowActive bool
	historyFollowPaused bool
}

type subscriber struct {
	ch      chan model.SessionEvent
	ready   bool
	backlog []model.SessionEvent
}

type Adapter struct {
	mu                   sync.RWMutex
	sessions             map[string]*sessionState
	deletedSessions      map[string]struct{}
	idempotency          map[string]idempotencyRecord
	streamMode           string
	cliBin               string
	cliArgs              []string
	cliTimeout           time.Duration
	mockFallback         bool
	historyEnabled       bool
	historyDir           string
	historyTTL           time.Duration
	historyActiveWindow  time.Duration
	historyTailInterval  time.Duration
	externalSessionIndex map[string]externalSessionInfo
	externalIndexAt      time.Time
}

type idempotencyRecord struct {
	job       model.RunJob
	expiresAt time.Time
}

type streamResult struct {
	deltaCount int
	err        error
}

type approvalRequiredError struct {
	threadID string
}

func (e approvalRequiredError) Error() string {
	return "codex cli requires interactive approval"
}

type commandOutputLine struct {
	source string
	line   string
}

type externalSessionInfo struct {
	detail model.SessionDetail
	path   string
}

type historyLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type historyEventMsg struct {
	Type   string `json:"type"`
	TurnID string `json:"turn_id"`
}

type historySessionMeta struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
	Source    string `json:"source"`
}

type historyMessage struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []historyMessageItem `json:"content"`
}

type historyMessageItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var _ adapter.AgentAdapter = (*Adapter)(nil)

func NewAdapter(seedPath string) (*Adapter, error) {
	streamMode, cliBin, cliArgs, cliTimeout, mockFallback := loadRuntimeOptions()
	historyEnabled, historyDir, historyTTL := loadHistoryOptions()
	historyActiveWindow := loadHistoryActiveWindow()
	historyTailInterval := loadHistoryTailInterval()

	now := time.Now().UTC()
	sess := &sessionState{
		detail: model.SessionDetail{
			Adapter:   adapterName,
			ID:        defaultSessionID,
			Title:     "MVP bootstrap stream",
			Status:    sessionStatusIdle,
			CreatedAt: now.Add(-2 * time.Hour),
			UpdatedAt: now.Add(-90 * time.Minute),
			Workspace: defaultWorkspace,
			Source:    defaultSource,
			Metadata: map[string]any{
				"model": "gpt-5-codex",
			},
		},
		events:      make([]model.SessionEvent, 0, 16),
		nextSeq:     1,
		subscribers: make(map[int]*subscriber),
	}

	seedEvents := loadSeedEvents(seedPath, sess.detail.CreatedAt)
	for _, ev := range seedEvents {
		ev.Seq = sess.nextSeq
		sess.nextSeq++
		sess.events = append(sess.events, ev)
		sess.detail.UpdatedAt = ev.Ts
	}

	if len(sess.events) == 0 {
		now = time.Now().UTC()
		sess.events = append(sess.events,
			model.SessionEvent{
				Adapter:   adapterName,
				SessionID: defaultSessionID,
				Seq:       1,
				Ts:        now.Add(-2 * time.Minute),
				Type:      "message.delta",
				Payload: map[string]any{
					"raw_type": "assistant_delta",
					"text":     "Gateway bootstrap complete. Ready for continue.",
				},
				Normalized: map[string]any{
					"role": "assistant",
					"text": "Gateway bootstrap complete. Ready for continue.",
					"done": false,
				},
			},
			model.SessionEvent{
				Adapter:   adapterName,
				SessionID: defaultSessionID,
				Seq:       2,
				Ts:        now.Add(-90 * time.Second),
				Type:      "message.done",
				Payload: map[string]any{
					"raw_type": "assistant_done",
				},
				Normalized: map[string]any{
					"role": "assistant",
					"text": "",
					"done": true,
				},
			},
		)
		sess.nextSeq = 3
		sess.detail.UpdatedAt = now.Add(-90 * time.Second)
	}

	return &Adapter{
		sessions:             map[string]*sessionState{defaultSessionID: sess},
		deletedSessions:      make(map[string]struct{}),
		idempotency:          make(map[string]idempotencyRecord),
		streamMode:           streamMode,
		cliBin:               cliBin,
		cliArgs:              cliArgs,
		cliTimeout:           cliTimeout,
		mockFallback:         mockFallback,
		historyEnabled:       historyEnabled,
		historyDir:           historyDir,
		historyTTL:           historyTTL,
		historyActiveWindow:  historyActiveWindow,
		historyTailInterval:  historyTailInterval,
		externalSessionIndex: make(map[string]externalSessionInfo),
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
		title = "New Session"
	}
	if workspace == "" {
		workspace = defaultNewWorkspace
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
	sessionID := fmt.Sprintf("sess_%d", now.UnixNano())

	sess := &sessionState{
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
				"model": "gpt-5-codex",
			},
		},
		events:      make([]model.SessionEvent, 0, 8),
		nextSeq:     1,
		subscribers: make(map[int]*subscriber),
	}

	a.mu.Lock()
	a.sessions[sessionID] = sess
	a.mu.Unlock()

	if seedPrompt != "" {
		a.appendUserEvent(sessionID, seedPrompt)
		ackPayload := map[string]any{
			"raw_type": "assistant_done",
			"text":     "会话已创建，可继续对话。",
		}
		ackNormalized := map[string]any{
			"role": "assistant",
			"text": "会话已创建，可继续对话。",
			"done": true,
		}
		a.appendEvent(sessionID, "message.done", ackPayload, ackNormalized)
	}

	return sess.detail, nil
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

	if a.isSessionDeleted(sessionID) {
		return model.ErrSessionNotFound
	}

	a.mu.RLock()
	_, loaded := a.sessions[sessionID]
	a.mu.RUnlock()

	exists := loaded
	if !exists {
		index, err := a.loadExternalSessionIndex(ctx)
		if err != nil {
			return err
		}
		_, exists = index[sessionID]
		if !exists {
			return model.ErrSessionNotFound
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, removed := a.deletedSessions[sessionID]; removed {
		return model.ErrSessionNotFound
	}
	a.deletedSessions[sessionID] = struct{}{}

	if s, ok := a.sessions[sessionID]; ok {
		for subID, sub := range s.subscribers {
			delete(s.subscribers, subID)
			close(sub.ch)
		}
	}
	delete(a.sessions, sessionID)
	delete(a.externalSessionIndex, sessionID)

	for key := range a.idempotency {
		if strings.HasPrefix(key, sessionID+"::") {
			delete(a.idempotency, key)
		}
	}
	return nil
}

func (a *Adapter) HealthCheck(ctx context.Context) (int64, error) {
	start := time.Now()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	a.mu.RLock()
	_ = len(a.sessions)
	a.mu.RUnlock()
	return time.Since(start).Milliseconds(), nil
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

	history, err := a.loadExternalSessionIndex(ctx)
	if err != nil {
		return model.PagedSessions{}, err
	}
	historyAliasToLocal := make(map[string]string)
	if len(history) > 0 {
		a.mu.Lock()
		threadToSessionID := make(map[string]string, len(a.sessions))
		for id, state := range a.sessions {
			if _, deleted := a.deletedSessions[id]; deleted {
				continue
			}
			if state == nil {
				continue
			}
			threadID := strings.TrimSpace(state.codexThread)
			if threadID == "" {
				threadID = inferSessionThreadID(state.detail.ID, state.detail.Metadata)
			}
			if threadID == "" {
				continue
			}
			existingID, exists := threadToSessionID[threadID]
			if !exists {
				threadToSessionID[threadID] = id
				continue
			}
			existingState, ok := a.sessions[existingID]
			if !ok || existingState == nil {
				threadToSessionID[threadID] = id
				continue
			}
			// Prefer local "cli" session over history-backed session for the same Codex thread.
			if isHistoryBackedSession(existingState.detail) && !isHistoryBackedSession(state.detail) {
				threadToSessionID[threadID] = id
			}
		}

		for historyID, info := range history {
			targetID := ""
			historyThreadID := inferSessionThreadID(info.detail.ID, info.detail.Metadata)
			if historyThreadID != "" {
				targetID = threadToSessionID[historyThreadID]
			}
			if targetID == "" {
				if _, exists := a.sessions[historyID]; exists {
					targetID = historyID
				}
			}
			if targetID == "" {
				continue
			}
			state, exists := a.sessions[targetID]
			if !exists || state == nil {
				continue
			}
			applyHistoryDetail(&state.detail, info, state.activeRuns > 0)
			historyAliasToLocal[historyID] = targetID
		}
		a.mu.Unlock()
	}

	itemMap := make(map[string]model.SessionSummary)
	a.mu.RLock()
	for id, s := range a.sessions {
		if _, deleted := a.deletedSessions[id]; deleted {
			continue
		}
		itemMap[s.detail.ID] = summarizeSession(s.detail)
	}
	a.mu.RUnlock()

	for id, info := range history {
		if a.isSessionDeleted(id) {
			continue
		}
		if _, aliased := historyAliasToLocal[id]; aliased {
			continue
		}
		if _, exists := itemMap[id]; exists {
			continue
		}
		itemMap[id] = summarizeSession(info.detail)
	}

	items := make([]model.SessionSummary, 0, len(itemMap))
	query := strings.ToLower(strings.TrimSpace(req.Query))
	for _, item := range itemMap {
		select {
		case <-ctx.Done():
			return model.PagedSessions{}, ctx.Err()
		default:
		}

		if query != "" && !strings.Contains(strings.ToLower(item.Title), query) {
			continue
		}
		if req.Workspace != "" && item.Workspace != req.Workspace {
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

	if err := a.ensureSessionLoaded(ctx, sessionID); err != nil {
		return model.SessionDetail{}, err
	}
	if history, err := a.loadExternalSessionIndex(ctx); err == nil {
		if info, ok := history[sessionID]; ok {
			a.mu.Lock()
			if s, exists := a.sessions[sessionID]; exists && isHistoryBackedSession(s.detail) {
				applyHistoryDetail(&s.detail, info, s.activeRuns > 0)
			}
			a.mu.Unlock()
		}
	}

	a.mu.RLock()
	s := a.sessions[sessionID]
	a.mu.RUnlock()
	return cloneSessionDetail(s.detail), nil
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

	if err := a.ensureSessionLoaded(ctx, req.SessionID); err != nil {
		return model.PagedEvents{}, err
	}

	a.mu.RLock()
	s := a.sessions[req.SessionID]
	filtered := make([]model.SessionEvent, 0, len(s.events))
	for _, ev := range s.events {
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

	return model.PagedEvents{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (a *Adapter) ContinueSession(ctx context.Context, req model.ContinueInput) (model.RunJob, error) {
	if err := ctx.Err(); err != nil {
		return model.RunJob{}, err
	}

	prompt := strings.TrimSpace(req.Prompt)
	if len(prompt) == 0 || len(prompt) > 8000 {
		return model.RunJob{}, fmt.Errorf("%w: prompt", model.ErrInvalidParam)
	}
	if a.streamMode == "real" {
		if _, err := exec.LookPath(a.cliBin); err != nil && !a.mockFallback {
			return model.RunJob{}, fmt.Errorf("codex cli not found: %w", err)
		}
	}

	if err := a.ensureSessionLoaded(ctx, req.SessionID); err != nil {
		return model.RunJob{}, err
	}

	a.mu.Lock()

	now := time.Now().UTC()
	for key, record := range a.idempotency {
		if !record.expiresAt.After(now) {
			delete(a.idempotency, key)
		}
	}
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		cacheKey := req.SessionID + "::" + key
		if record, exists := a.idempotency[cacheKey]; exists && record.expiresAt.After(now) {
			a.mu.Unlock()
			return record.job, nil
		}
	}
	if sess, exists := a.sessions[req.SessionID]; exists {
		sess.activeRuns++
		sess.detail.Status = sessionStatusRunning
		sess.detail.UpdatedAt = now
		if isHistoryBackedSession(sess.detail) {
			sess.historyFollowPaused = true
		}
	}

	startedAt := now
	job := model.RunJob{
		JobID:     fmt.Sprintf("job_%d", startedAt.UnixNano()),
		Adapter:   adapterName,
		SessionID: req.SessionID,
		Status:    "accepted",
		StartedAt: startedAt,
	}

	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		cacheKey := req.SessionID + "::" + key
		a.idempotency[cacheKey] = idempotencyRecord{
			job:       job,
			expiresAt: now.Add(idempotencyTTL),
		}
	}
	a.mu.Unlock()

	go func() {
		defer a.finishSessionRun(req.SessionID)
		defer a.resumeHistoryFollow(req.SessionID)
		a.emitContinueEvents(ctx, req.SessionID, prompt)
	}()
	return job, nil
}

func (a *Adapter) Subscribe(ctx context.Context, sessionID string, fromSeq int64) (<-chan model.SessionEvent, func(), error) {
	if err := a.ensureSessionLoaded(ctx, sessionID); err != nil {
		return nil, nil, err
	}

	a.mu.Lock()
	s := a.sessions[sessionID]

	subID := s.nextSubID
	s.nextSubID++
	sub := &subscriber{
		ch:      make(chan model.SessionEvent, 64),
		ready:   false,
		backlog: make([]model.SessionEvent, 0, 16),
	}
	s.subscribers[subID] = sub
	historyFollow := isHistoryBackedSession(s.detail)

	history := make([]model.SessionEvent, 0, len(s.events))
	for _, ev := range s.events {
		if ev.Seq > fromSeq {
			history = append(history, ev)
		}
	}
	a.mu.Unlock()

	if historyFollow {
		a.ensureHistoryFollower(sessionID)
	}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			a.mu.Lock()
			if state, exists := a.sessions[sessionID]; exists {
				if sub, found := state.subscribers[subID]; found {
					delete(state.subscribers, subID)
					close(sub.ch)
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

	// Keep subscriber in "not ready" state while flushing backlog, so
	// appendEvent() only appends to backlog and cannot interleave newer
	// events before older buffered ones.
	for {
		a.mu.Lock()
		state, exists := a.sessions[sessionID]
		if !exists {
			a.mu.Unlock()
			unsubscribe()
			return nil, nil, model.ErrSessionNotFound
		}

		current, found := state.subscribers[subID]
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

func (a *Adapter) ensureSessionLoaded(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.ErrSessionNotFound
	}
	if a.isSessionDeleted(sessionID) {
		return model.ErrSessionNotFound
	}

	a.mu.RLock()
	_, exists := a.sessions[sessionID]
	a.mu.RUnlock()
	if exists {
		return nil
	}

	index, err := a.loadExternalSessionIndex(ctx)
	if err != nil {
		return err
	}
	info, ok := index[sessionID]
	if !ok {
		return model.ErrSessionNotFound
	}
	if a.isSessionDeleted(sessionID) {
		return model.ErrSessionNotFound
	}

	events, offset, err := loadHistorySessionEvents(ctx, info.path, sessionID)
	if err != nil {
		return fmt.Errorf("load history session %s failed: %w", sessionID, err)
	}

	state := &sessionState{
		detail:        cloneSessionDetail(info.detail),
		events:        events,
		nextSeq:       1,
		subscribers:   make(map[int]*subscriber),
		codexThread:   inferSessionThreadID(info.detail.ID, info.detail.Metadata),
		historyPath:   info.path,
		historyOffset: offset,
	}
	if len(events) > 0 {
		state.nextSeq = events[len(events)-1].Seq + 1
		state.detail.CreatedAt = events[0].Ts
		state.detail.UpdatedAt = events[len(events)-1].Ts
	}

	a.mu.Lock()
	if _, removed := a.deletedSessions[sessionID]; removed {
		a.mu.Unlock()
		return model.ErrSessionNotFound
	}
	if _, loaded := a.sessions[sessionID]; !loaded {
		a.sessions[sessionID] = state
	}
	a.mu.Unlock()
	return nil
}

func (a *Adapter) loadExternalSessionIndex(ctx context.Context) (map[string]externalSessionInfo, error) {
	if !a.historyEnabled || strings.TrimSpace(a.historyDir) == "" {
		return map[string]externalSessionInfo{}, nil
	}

	now := time.Now()
	a.mu.RLock()
	cached := a.externalSessionIndex
	loadedAt := a.externalIndexAt
	ttl := a.historyTTL
	a.mu.RUnlock()

	if ttl <= 0 {
		ttl = defaultHistoryTTL
	}

	if !loadedAt.IsZero() && now.Sub(loadedAt) < ttl {
		return cloneExternalSessionIndex(a.filterDeletedExternal(cached)), nil
	}

	fresh := make(map[string]externalSessionInfo)
	err := filepath.WalkDir(a.historyDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}

		info, ok := readHistorySessionSummary(path, a.historyActiveWindow)
		if !ok {
			return nil
		}
		prev, exists := fresh[info.detail.ID]
		if exists && prev.detail.UpdatedAt.After(info.detail.UpdatedAt) {
			return nil
		}
		fresh[info.detail.ID] = info
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	fresh = a.filterDeletedExternal(fresh)

	a.mu.Lock()
	a.externalSessionIndex = fresh
	a.externalIndexAt = time.Now().UTC()
	a.mu.Unlock()
	return cloneExternalSessionIndex(fresh), nil
}

func cloneExternalSessionIndex(source map[string]externalSessionInfo) map[string]externalSessionInfo {
	out := make(map[string]externalSessionInfo, len(source))
	for id, item := range source {
		out[id] = externalSessionInfo{
			detail: cloneSessionDetail(item.detail),
			path:   item.path,
		}
	}
	return out
}

func (a *Adapter) filterDeletedExternal(source map[string]externalSessionInfo) map[string]externalSessionInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.deletedSessions) == 0 {
		return cloneExternalSessionIndex(source)
	}
	out := make(map[string]externalSessionInfo, len(source))
	for id, item := range source {
		if _, deleted := a.deletedSessions[id]; deleted {
			continue
		}
		out[id] = externalSessionInfo{
			detail: cloneSessionDetail(item.detail),
			path:   item.path,
		}
	}
	return out
}

func (a *Adapter) isSessionDeleted(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	a.mu.RLock()
	_, deleted := a.deletedSessions[sessionID]
	a.mu.RUnlock()
	return deleted
}

func (a *Adapter) ensureHistoryFollower(sessionID string) {
	interval := a.historyTailInterval
	if interval <= 0 {
		return
	}
	a.mu.Lock()
	state, ok := a.sessions[sessionID]
	if !ok || state.historyFollowActive || state.historyPath == "" || !isHistoryBackedSession(state.detail) {
		a.mu.Unlock()
		return
	}
	state.historyFollowActive = true
	a.mu.Unlock()

	go a.followHistory(sessionID, interval)
}

func (a *Adapter) followHistory(sessionID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		a.mu.RLock()
		state, ok := a.sessions[sessionID]
		if !ok {
			a.mu.RUnlock()
			return
		}
		if state.historyFollowPaused {
			a.mu.RUnlock()
			continue
		}
		if len(state.subscribers) == 0 {
			a.mu.RUnlock()
			a.mu.Lock()
			if current, exists := a.sessions[sessionID]; exists {
				current.historyFollowActive = false
			}
			a.mu.Unlock()
			return
		}
		path := state.historyPath
		offset := state.historyOffset
		a.mu.RUnlock()

		if path == "" {
			a.mu.Lock()
			if current, exists := a.sessions[sessionID]; exists {
				current.historyFollowActive = false
			}
			a.mu.Unlock()
			return
		}

		events, nextOffset, err := readHistoryAppendedEvents(path, sessionID, offset)
		if err != nil {
			continue
		}
		if nextOffset != offset {
			a.mu.Lock()
			if current, exists := a.sessions[sessionID]; exists {
				current.historyOffset = nextOffset
			}
			a.mu.Unlock()
		}
		for _, ev := range events {
			a.appendHistoryMessage(sessionID, ev.role, ev.text)
		}
	}
}

func (a *Adapter) resumeHistoryFollow(sessionID string) {
	var path string
	var paused bool
	a.mu.Lock()
	if state, ok := a.sessions[sessionID]; ok {
		paused = state.historyFollowPaused
		path = state.historyPath
		state.historyFollowPaused = false
	}
	a.mu.Unlock()
	if !paused || path == "" {
		return
	}
	if size, err := historyFileSize(path); err == nil {
		a.mu.Lock()
		if state, ok := a.sessions[sessionID]; ok {
			state.historyOffset = size
		}
		a.mu.Unlock()
	}
}

type historyParsedEvent struct {
	role string
	text string
}

func readHistoryAppendedEvents(path, sessionID string, offset int64) ([]historyParsedEvent, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	if len(buf) == 0 {
		return nil, offset, nil
	}

	data := string(buf)
	lines := strings.Split(data, "\n")
	nextOffset := offset + int64(len(buf))
	if !strings.HasSuffix(data, "\n") {
		incomplete := lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		nextOffset -= int64(len(incomplete))
	}

	events := make([]historyParsedEvent, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		var item historyLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item.Type != "response_item" {
			continue
		}
		role, text, ok := extractHistoryMessage(item.Payload)
		if !ok {
			continue
		}
		if role == "user" && isBootstrapHistoryText(text) {
			continue
		}
		events = append(events, historyParsedEvent{role: role, text: text})
	}

	return events, nextOffset, nil
}

func historyFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func readHistorySessionSummary(path string, activeWindow time.Duration) (externalSessionInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return externalSessionInfo{}, false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return externalSessionInfo{}, false
	}
	modTime := stat.ModTime().UTC()

	var (
		sessionID string
		createdAt time.Time
		updatedAt time.Time
		workspace string
		source    string
		title     string
	)
	activeTurnID := ""
	activeTurnAt := time.Time{}
	sawTaskEvents := false
	lastTaskStartAt := time.Time{}
	lastTaskEndAt := time.Time{}

	scanner := newJSONLScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item historyLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}

		ts := parseHistoryTime(item.Timestamp)
		if !ts.IsZero() {
			if createdAt.IsZero() || ts.Before(createdAt) {
				createdAt = ts
			}
			if ts.After(updatedAt) {
				updatedAt = ts
			}
			// While a turn is active, treat any newer session signal as liveness
			// heartbeat, not only the initial task_started timestamp.
			if activeTurnID != "" && (activeTurnAt.IsZero() || ts.After(activeTurnAt)) {
				activeTurnAt = ts
			}
		}

		switch item.Type {
		case "session_meta":
			var meta historySessionMeta
			if err := json.Unmarshal(item.Payload, &meta); err != nil {
				continue
			}
			if strings.TrimSpace(meta.ID) != "" {
				sessionID = strings.TrimSpace(meta.ID)
			}
			if strings.TrimSpace(meta.CWD) != "" {
				workspace = strings.TrimSpace(meta.CWD)
			}
			if strings.TrimSpace(meta.Source) != "" {
				source = strings.TrimSpace(meta.Source)
			}
			metaTS := parseHistoryTime(meta.Timestamp)
			if !metaTS.IsZero() {
				if createdAt.IsZero() || metaTS.Before(createdAt) {
					createdAt = metaTS
				}
				if metaTS.After(updatedAt) {
					updatedAt = metaTS
				}
			}
		case "response_item":
			role, text, ok := extractHistoryMessage(item.Payload)
			if !ok || role != "user" || title != "" {
				continue
			}
			candidate := toSessionTitle(text)
			if candidate != "" {
				title = candidate
			}
		case "event_msg":
			var evt historyEventMsg
			if err := json.Unmarshal(item.Payload, &evt); err != nil {
				continue
			}
			eventType := strings.ToLower(strings.TrimSpace(evt.Type))
			turnID := strings.TrimSpace(evt.TurnID)
			if eventType == "" {
				continue
			}

			isTaskStart := eventType == "task_started" ||
				eventType == "turn_started" ||
				strings.HasSuffix(eventType, "_started") ||
				strings.HasSuffix(eventType, ".started")
			isTaskEnd := eventType == "task_complete" ||
				eventType == "task_completed" ||
				eventType == "turn_aborted" ||
				eventType == "task_aborted" ||
				eventType == "task_failed" ||
				eventType == "task_error" ||
				strings.HasSuffix(eventType, "aborted") ||
				strings.HasSuffix(eventType, "complete") ||
				strings.HasSuffix(eventType, "completed") ||
				strings.HasSuffix(eventType, "failed") ||
				strings.HasSuffix(eventType, "error")
			if !isTaskStart && !isTaskEnd {
				continue
			}

			sawTaskEvents = true
			if ts.IsZero() {
				ts = updatedAt
			}
			if ts.IsZero() {
				ts = modTime
			}

			if isTaskStart {
				activeTurnID = turnID
				activeTurnAt = ts
				if !ts.IsZero() && (lastTaskStartAt.IsZero() || ts.After(lastTaskStartAt)) {
					lastTaskStartAt = ts
				}
				continue
			}
			if !ts.IsZero() && (lastTaskEndAt.IsZero() || ts.After(lastTaskEndAt)) {
				lastTaskEndAt = ts
			}
			// A terminal event without turn_id (or with a mismatched one) still
			// means the active turn likely finished, so clear running state.
			if turnID == "" || activeTurnID == "" || turnID == activeTurnID || (!ts.IsZero() && (activeTurnAt.IsZero() || !ts.Before(activeTurnAt))) {
				activeTurnID = ""
				activeTurnAt = time.Time{}
			}
		}
	}

	if strings.TrimSpace(sessionID) == "" {
		return externalSessionInfo{}, false
	}
	if updatedAt.IsZero() {
		updatedAt = modTime
	}
	if !modTime.IsZero() && modTime.After(updatedAt) {
		updatedAt = modTime
	}
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	if activeTurnID != "" && !modTime.IsZero() && modTime.After(activeTurnAt) {
		activeTurnAt = modTime
	}
	if source == "" {
		source = defaultHistorySource
	}
	if title == "" {
		title = sessionID
	}

	hasRecentActiveTurn := false
	if activeTurnID != "" || !activeTurnAt.IsZero() {
		if activeWindow <= 0 {
			hasRecentActiveTurn = true
		} else {
			turnTS := activeTurnAt
			if turnTS.IsZero() {
				turnTS = updatedAt
			}
			if !turnTS.IsZero() && time.Since(turnTS) <= activeWindow {
				hasRecentActiveTurn = true
			}
		}
	}

	status := sessionStatusIdle
	if hasRecentActiveTurn {
		status = sessionStatusRunning
	} else if !lastTaskStartAt.IsZero() && (lastTaskEndAt.IsZero() || lastTaskStartAt.After(lastTaskEndAt)) {
		if activeWindow <= 0 {
			status = sessionStatusRunning
		} else if !updatedAt.IsZero() && time.Since(updatedAt) <= activeWindow {
			// A task start newer than the latest terminal event plus recent log
			// activity indicates an active external run, even if terminal events
			// are delayed or temporarily missing.
			status = sessionStatusRunning
		}
	} else if !sawTaskEvents && activeWindow > 0 && !updatedAt.IsZero() {
		if time.Since(updatedAt) <= activeWindow {
			status = sessionStatusRunning
		}
	}

	detail := model.SessionDetail{
		Adapter:   adapterName,
		ID:        sessionID,
		Title:     title,
		Status:    status,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Workspace: workspace,
		Source:    source,
		Metadata: map[string]any{
			"origin":       "codex_history",
			"rollout_path": path,
		},
	}
	if looksLikeThreadID(sessionID) {
		detail.Metadata["codex_thread_id"] = sessionID
	}

	return externalSessionInfo{
		detail: detail,
		path:   path,
	}, true
}

func loadHistorySessionEvents(ctx context.Context, path, sessionID string) ([]model.SessionEvent, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	events := make([]model.SessionEvent, 0, 128)
	seq := int64(1)
	lastTS := time.Time{}

	scanner := newJSONLScanner(f)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item historyLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item.Type != "response_item" {
			continue
		}

		role, text, ok := extractHistoryMessage(item.Payload)
		if !ok {
			continue
		}
		if role == "user" && isBootstrapHistoryText(text) {
			continue
		}

		eventTS := parseHistoryTime(item.Timestamp)
		if eventTS.IsZero() {
			if lastTS.IsZero() {
				eventTS = time.Now().UTC()
			} else {
				eventTS = lastTS.Add(time.Millisecond)
			}
		}
		if !lastTS.IsZero() && eventTS.Before(lastTS) {
			eventTS = lastTS.Add(time.Millisecond)
		}
		lastTS = eventTS

		eventType := "message.done"
		if role == "user" {
			eventType = "message.user"
		}

		events = append(events, model.SessionEvent{
			Adapter:   adapterName,
			SessionID: sessionID,
			Seq:       seq,
			Ts:        eventTS,
			Type:      eventType,
			Payload: map[string]any{
				"raw_type": "history_message",
				"source":   defaultHistorySource,
				"text":     text,
			},
			Normalized: map[string]any{
				"role": role,
				"text": text,
				"done": true,
			},
		})
		seq++
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		if stat, statErr := f.Stat(); statErr == nil {
			offset = stat.Size()
		}
	}
	return events, offset, nil
}

func extractHistoryMessage(payload json.RawMessage) (string, string, bool) {
	var msg historyMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return "", "", false
	}
	if strings.TrimSpace(msg.Type) != "message" {
		return "", "", false
	}

	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role != "user" && role != "assistant" {
		return "", "", false
	}

	parts := make([]string, 0, len(msg.Content))
	for _, item := range msg.Content {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "", "", false
	}
	return role, strings.Join(parts, "\n"), true
}

func parseHistoryTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

func toSessionTitle(text string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if clean == "" {
		return ""
	}
	if isBootstrapHistoryText(clean) {
		return ""
	}

	const maxRunes = 72
	runes := []rune(clean)
	if len(runes) <= maxRunes {
		return clean
	}
	return string(runes[:maxRunes]) + "..."
}

func newJSONLScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	return scanner
}

func isBootstrapHistoryText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "agents.md instructions") ||
		strings.Contains(lower, "<environment_context>") ||
		strings.Contains(lower, "<permissions instructions>")
}

func (a *Adapter) emitContinueEvents(ctx context.Context, sessionID, prompt string) {
	a.appendUserEvent(sessionID, prompt)
	if a.streamMode == "real" {
		result := a.emitRealContinueEvents(ctx, sessionID, prompt)
		if result.err == nil {
			return
		}
		if a.mockFallback && result.deltaCount == 0 {
			a.emitMockContinueEvents(ctx, sessionID, prompt)
			return
		}
		a.appendAssistantFailure(sessionID, result.err)
		return
	}
	a.emitMockContinueEvents(ctx, sessionID, prompt)
}

func (a *Adapter) emitMockContinueEvents(ctx context.Context, sessionID, prompt string) {
	a.appendAssistantAction(sessionID, "正在生成回复", map[string]any{
		"raw_type": "assistant_action",
		"source":   "mock",
	})

	parts := []string{
		"收到继续请求，开始执行: ",
		prompt,
	}

	for i, part := range parts {
		if ctx.Err() != nil {
			return
		}
		a.appendAssistantDelta(sessionID, part, map[string]any{
			"raw_type": "assistant_delta",
			"source":   "mock",
		})
		if i < len(parts)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(continueChunkWait):
			}
		}
	}

	if ctx.Err() != nil {
		return
	}
	a.appendAssistantDone(sessionID, "", map[string]any{
		"raw_type": "assistant_done",
		"source":   "mock",
	})
}

func (a *Adapter) emitRealContinueEvents(ctx context.Context, sessionID, prompt string) streamResult {
	runCtx := ctx
	cancel := func() {}
	if a.cliTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, a.cliTimeout)
	}
	defer cancel()

	threadID := a.getSessionThreadID(sessionID)
	args := buildCLICommandArgs(a.cliArgs, prompt, threadID)

	cmd := exec.CommandContext(runCtx, a.cliBin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return streamResult{err: fmt.Errorf("create stdout pipe failed: %w", err)}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return streamResult{err: fmt.Errorf("create stderr pipe failed: %w", err)}
	}
	if err := cmd.Start(); err != nil {
		return streamResult{err: fmt.Errorf("start codex cli failed: %w", err)}
	}

	lineCh := make(chan commandOutputLine, 128)
	var scanWG sync.WaitGroup
	scanWG.Add(2)
	go func() {
		defer scanWG.Done()
		streamCommandOutput(stdout, "stdout", lineCh)
	}()
	go func() {
		defer scanWG.Done()
		streamCommandOutput(stderr, "stderr", lineCh)
	}()
	go func() {
		scanWG.Wait()
		close(lineCh)
	}()

	deltaCount := 0
	errLines := make([]string, 0, 8)
	approvalBlocked := false
	approvalHintSent := false
	detectedThreadID := strings.TrimSpace(threadID)
	sawCompletionEvent := false

	for item := range lineCh {
		if item.source == "stderr" {
			if detectCLIApprovalPromptLine(item.line) {
				approvalBlocked = true
				if !approvalHintSent {
					approvalHintSent = true
					a.appendAssistantAction(sessionID, "检测到该会话需要人工确认，Web 暂不支持确认交互", map[string]any{
						"raw_type": "approval_required",
						"source":   "cli",
					})
				}
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				continue
			}
			if normalized := normalizeCLIErrorLine(item.line); normalized != "" {
				errLines = append(errLines, normalized)
			}
			continue
		}

		eventType, chunks, eventErr, approvalRequired := parseCLIJSONLine(item.line)
		if eventErr != "" {
			errLines = append(errLines, eventErr)
		}
		if isCLICompletionEvent(eventType, item.line) {
			sawCompletionEvent = true
		}
		if tid := parseCLIThreadID(eventType, item.line); tid != "" {
			a.setSessionThreadID(sessionID, tid)
			detectedThreadID = tid
		}
		if approvalRequired {
			approvalBlocked = true
			if !approvalHintSent {
				approvalHintSent = true
				a.appendAssistantAction(sessionID, "检测到该会话需要人工确认，Web 暂不支持确认交互", map[string]any{
					"raw_type": eventType,
					"source":   "cli",
				})
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			continue
		}
		if action := summarizeCLIAction(eventType, item.line); action != "" {
			a.appendAssistantAction(sessionID, action, map[string]any{
				"raw_type": eventType,
				"source":   "cli",
			})
		}
		if approvalBlocked {
			continue
		}
		for _, chunk := range chunks {
			if ctx.Err() != nil {
				return streamResult{deltaCount: deltaCount, err: ctx.Err()}
			}
			a.appendAssistantDelta(sessionID, chunk, map[string]any{
				"raw_type": eventType,
				"source":   "cli",
			})
			deltaCount++
		}
	}

	waitErr := cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		if sawCompletionEvent && deltaCount > 0 {
			a.appendAssistantDone(sessionID, "", map[string]any{
				"raw_type": "assistant_done",
				"source":   "cli",
				"reason":   "synthesized_after_timeout",
			})
			return streamResult{deltaCount: deltaCount}
		}
		return streamResult{deltaCount: deltaCount, err: fmt.Errorf("codex cli timeout after %s", a.cliTimeout)}
	}
	if ctx.Err() != nil {
		return streamResult{deltaCount: deltaCount, err: ctx.Err()}
	}
	if approvalBlocked {
		if detectedThreadID == "" {
			detectedThreadID = a.getSessionThreadID(sessionID)
		}
		return streamResult{
			deltaCount: deltaCount,
			err: approvalRequiredError{
				threadID: strings.TrimSpace(detectedThreadID),
			},
		}
	}
	if waitErr != nil {
		msg := "codex cli exited with error"
		if len(errLines) > 0 {
			msg = msg + ": " + strings.Join(lastN(errLines, 3), " | ")
		}
		return streamResult{deltaCount: deltaCount, err: fmt.Errorf("%s: %w", msg, waitErr)}
	}
	if deltaCount == 0 {
		return streamResult{deltaCount: 0, err: fmt.Errorf("codex cli returned no assistant delta")}
	}

	a.appendAssistantDone(sessionID, "", map[string]any{
		"raw_type": "assistant_done",
		"source":   "cli",
	})
	return streamResult{deltaCount: deltaCount}
}

func streamCommandOutput(r io.Reader, source string, out chan<- commandOutputLine) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out <- commandOutputLine{
			source: source,
			line:   line,
		}
	}
}

func normalizeCLIErrorLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	// Codex CLI startup warns frequently in sandboxed env; do not escalate these.
	if strings.HasPrefix(line, "WARNING:") {
		return ""
	}
	return line
}

func summarizeCLIAction(eventType, rawLine string) string {
	lowerType := strings.ToLower(strings.TrimSpace(eventType))
	switch lowerType {
	case "thread.started":
		return "会话线程已启动"
	case "turn.started":
		return "开始处理本轮请求"
	case "turn.completed":
		return "本轮请求处理完成"
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(rawLine), &event); err != nil {
		return ""
	}

	if strings.Contains(lowerType, "error") {
		return "执行出现异常"
	}

	if lowerType == "item.started" || lowerType == "item.completed" {
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		itemType = strings.ToLower(strings.TrimSpace(itemType))
		completed := lowerType == "item.completed"
		switch itemType {
		case "reasoning":
			if completed {
				return "推理完成"
			}
			return "正在推理"
		case "function_call", "tool_call", "custom_tool_call", "mcp_tool_call":
			name := firstNonEmptyString(item, "name", "tool_name", "function_name")
			name = trimRunes(name, 56)
			return formatActionLabel(classifyActionKind(itemType, name, ""), name, completed)
		case "command_execution":
			command := trimRunes(firstNonEmptyString(item, "command"), 56)
			kind := classifyActionKind(itemType, "", command)
			exitCode, hasExitCode := asInt64(item["exit_code"])
			if completed {
				if command == "" {
					if hasExitCode {
						return fmt.Sprintf("%s ✓ (exit %d)", kind, exitCode)
					}
					return kind + " ✓"
				}
				if hasExitCode {
					return fmt.Sprintf("%s ✓ (exit %d) · %s", kind, exitCode, command)
				}
				return kind + " ✓ · " + command
			}
			if command == "" {
				return kind
			}
			return kind + " · " + command
		case "agent_message", "message":
			if completed {
				return "回复片段已生成"
			}
			return "正在生成回复片段"
		}
		name := trimRunes(firstNonEmptyString(item, "name", "tool_name", "function_name"), 56)
		if name != "" {
			return formatActionLabel(classifyActionKind(itemType, name, ""), name, completed)
		}
		if !completed {
			return "正在处理任务项"
		}
		return "任务项处理完成"
	}
	return ""
}

func formatActionLabel(kind, name string, completed bool) string {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if kind == "" {
		kind = "Called"
	}
	if completed {
		if name == "" {
			return kind + " ✓"
		}
		return kind + " ✓ · " + name
	}
	if name == "" {
		return kind
	}
	return kind + " · " + name
}

func classifyActionKind(itemType, name, command string) string {
	itemType = strings.ToLower(strings.TrimSpace(itemType))
	name = strings.ToLower(strings.TrimSpace(name))
	command = strings.ToLower(strings.TrimSpace(command))
	if itemType == "command_execution" || command != "" {
		if isEditedCommand(command) {
			return "Edited"
		}
		if isExploredCommand(command) {
			return "Explored"
		}
		return "Run"
	}
	if isEditedActionName(name) {
		return "Edited"
	}
	if isExploredActionName(name) {
		return "Explored"
	}
	return "Called"
}

func isEditedActionName(name string) bool {
	if name == "" {
		return false
	}
	editNeedles := []string{
		"apply_patch",
		"update",
		"edit",
		"write",
		"create",
		"delete",
		"remove",
		"rename",
		"move",
		"append",
		"insert",
		"replace",
		"modify",
	}
	for _, needle := range editNeedles {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}

func isExploredActionName(name string) bool {
	if name == "" {
		return false
	}
	exploreNeedles := []string{
		"read",
		"list",
		"search",
		"find",
		"fetch",
		"open",
		"query",
		"scan",
		"snapshot",
		"screenshot",
		"inspect",
		"view",
		"show",
		"get",
		"discover",
	}
	for _, needle := range exploreNeedles {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}

func isEditedCommand(command string) bool {
	probe := normalizeCommandForMatch(command)
	if probe == "" {
		return false
	}
	editNeedles := []string{
		"apply_patch",
		" sed -i ",
		" perl -i ",
		" cat > ",
		" cat >> ",
		" tee ",
		" mv ",
		" cp ",
		" rm ",
		" mkdir ",
		" touch ",
		" chmod ",
		" chown ",
		" git add ",
		" git commit ",
		" git mv ",
		" git rm ",
		" gofmt -w ",
		" prettier --write ",
	}
	for _, needle := range editNeedles {
		if strings.Contains(probe, needle) {
			return true
		}
	}
	return false
}

func isExploredCommand(command string) bool {
	probe := normalizeCommandForMatch(command)
	if probe == "" {
		return false
	}
	exploreNeedles := []string{
		" ls ",
		" tree ",
		" pwd ",
		" rg ",
		" grep ",
		" find ",
		" cat ",
		" sed -n",
		" head ",
		" tail ",
		" wc ",
		" git status",
		" git diff",
		" git log",
		" git show",
		" ps ",
		" lsof ",
		" ss ",
		" stat ",
	}
	for _, needle := range exploreNeedles {
		if strings.Contains(probe, needle) {
			return true
		}
	}
	return false
}

func normalizeCommandForMatch(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"'", " ",
		"\"", " ",
		"`", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		";", " ",
		"|", " ",
		"&", " ",
		"\n", " ",
		"\t", " ",
	)
	command = replacer.Replace(command)
	command = strings.Join(strings.Fields(command), " ")
	if command == "" {
		return ""
	}
	return " " + command + " "
}

func firstNonEmptyString(node map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := node[key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func asInt64(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		return int64(value), true
	case float32:
		return int64(value), true
	case float64:
		return int64(value), true
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return parsed, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func trimRunes(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "..."
}

func buildCLICommandArgs(base []string, prompt, threadID string) []string {
	prompt = strings.TrimSpace(prompt)
	threadID = strings.TrimSpace(threadID)

	if threadID == "" {
		args := make([]string, 0, len(base)+1)
		if len(base) == 0 {
			args = append(args, "exec", "--json")
		} else {
			args = append(args, base...)
		}
		args = append(args, prompt)
		return args
	}

	args := make([]string, 0, len(base)+3)
	if len(base) == 0 {
		args = append(args, "exec", "resume", "--json")
		args = append(args, threadID, prompt)
		return args
	}

	args = append(args, base...)
	if args[0] != "exec" {
		args = append([]string{"exec"}, args...)
	}
	if len(args) < 2 || args[1] != "resume" {
		args = append(args[:1], append([]string{"resume"}, args[1:]...)...)
	}
	args = append(args, threadID, prompt)
	return args
}

func parseCLIThreadID(eventType, rawLine string) string {
	if strings.ToLower(strings.TrimSpace(eventType)) != "thread.started" {
		return ""
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(rawLine), &event); err != nil {
		return ""
	}

	if tid, _ := event["thread_id"].(string); strings.TrimSpace(tid) != "" {
		return strings.TrimSpace(tid)
	}
	if threadObj, ok := event["thread"].(map[string]any); ok {
		if tid, _ := threadObj["id"].(string); strings.TrimSpace(tid) != "" {
			return strings.TrimSpace(tid)
		}
	}
	return ""
}

func parseCLIJSONLine(line string) (string, []string, string, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		if shouldUsePlainLineAsDelta(line) {
			return "stdout.text", splitDeltaText(line), "", detectCLIApprovalPromptLine(line)
		}
		return "", nil, "", false
	}

	eventType, _ := event["type"].(string)
	if detectCLIApprovalPromptEvent(eventType, event) {
		return eventType, nil, "", true
	}
	lowerType := strings.ToLower(strings.TrimSpace(eventType))
	if strings.Contains(lowerType, "error") {
		if msg, _ := event["message"].(string); strings.TrimSpace(msg) != "" {
			return eventType, nil, msg, false
		}
		if errObj, ok := event["error"].(map[string]any); ok {
			if msg, _ := errObj["message"].(string); strings.TrimSpace(msg) != "" {
				return eventType, nil, msg, false
			}
		}
		return eventType, nil, "codex cli returned error event", false
	}
	if strings.HasPrefix(lowerType, "thread.") || lowerType == "turn.started" {
		return eventType, nil, "", false
	}
	if lowerType == "item.completed" {
		if item, ok := event["item"].(map[string]any); ok {
			itemType, _ := item["type"].(string)
			itemType = strings.ToLower(strings.TrimSpace(itemType))
			if itemType == "reasoning" {
				return eventType, nil, "", false
			}
			if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
				return eventType, splitDeltaText(text), "", false
			}
		}
	}

	chunks := extractAssistantChunks(event)
	return eventType, chunks, "", false
}

func isCLICompletionEvent(eventType, rawLine string) bool {
	lowerType := strings.ToLower(strings.TrimSpace(eventType))
	switch lowerType {
	case "turn.completed", "response.completed", "task_complete", "task_completed":
		return true
	}
	if lowerType != "event_msg" {
		return false
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(rawLine), &event); err != nil {
		return false
	}
	payload, _ := event["payload"].(map[string]any)
	payloadType := strings.ToLower(strings.TrimSpace(firstNonEmptyString(payload, "type", "event_type")))
	return payloadType == "task_complete" || payloadType == "task_completed"
}

func detectCLIApprovalPromptEvent(eventType string, event map[string]any) bool {
	lowerType := strings.ToLower(strings.TrimSpace(eventType))
	if strings.Contains(lowerType, "approval") ||
		strings.Contains(lowerType, "confirm") ||
		strings.Contains(lowerType, "requires_action") ||
		strings.Contains(lowerType, "awaiting_input") {
		return true
	}

	return detectCLIApprovalPromptNode(event)
}

func detectCLIApprovalPromptNode(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		for key, raw := range node {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if isApprovalFieldKey(lowerKey) && isTruthy(raw) {
				return true
			}
			if isStatusFieldKey(lowerKey) {
				if text, ok := raw.(string); ok && detectCLIApprovalPromptLine(text) {
					return true
				}
			}
			if detectCLIApprovalPromptNode(raw) {
				return true
			}
		}
	case []any:
		for _, item := range node {
			if detectCLIApprovalPromptNode(item) {
				return true
			}
		}
	}
	return false
}

func isApprovalFieldKey(key string) bool {
	switch key {
	case "approval_required", "requires_approval", "needs_approval", "awaiting_approval", "pending_approval":
		return true
	default:
		return false
	}
}

func isStatusFieldKey(key string) bool {
	switch key {
	case "status", "state", "phase", "reason", "message", "prompt", "detail":
		return true
	default:
		return false
	}
}

func detectCLIApprovalPromptLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(lower)
	if strings.Contains(lower, "approval policy is currently never") {
		return false
	}

	needles := []string{
		"awaiting approval",
		"pending approval",
		"approval required",
		"requires approval",
		"requires user approval",
		"approve this command",
		"approve command",
		"press enter to approve",
		"approve? (y/n)",
		"[y/n]",
		"(y/n)",
		"等待确认",
		"需要确认",
		"需要人工确认",
		"请确认后继续",
	}
	for _, needle := range needles {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func isTruthy(raw any) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "y":
			return true
		default:
			return false
		}
	case int:
		return value != 0
	case int8:
		return value != 0
	case int16:
		return value != 0
	case int32:
		return value != 0
	case int64:
		return value != 0
	case uint:
		return value != 0
	case uint8:
		return value != 0
	case uint16:
		return value != 0
	case uint32:
		return value != 0
	case uint64:
		return value != 0
	case float32:
		return value != 0
	case float64:
		return value != 0
	default:
		return false
	}
}

func shouldUsePlainLineAsDelta(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, "WARNING:") {
		return false
	}
	if strings.Contains(line, " ERROR ") {
		return false
	}
	return true
}

func extractAssistantChunks(payload map[string]any) []string {
	allowedKeys := map[string]struct{}{
		"delta":       {},
		"text":        {},
		"output_text": {},
		"content":     {},
	}

	raw := make([]string, 0, 8)
	collectTextByKey(payload, allowedKeys, &raw)
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range splitDeltaText(item) {
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

func collectTextByKey(v any, allowed map[string]struct{}, out *[]string) {
	switch node := v.(type) {
	case map[string]any:
		for key, child := range node {
			if _, ok := allowed[strings.ToLower(key)]; ok {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					*out = append(*out, text)
				}
			}
			collectTextByKey(child, allowed, out)
		}
	case []any:
		for _, child := range node {
			collectTextByKey(child, allowed, out)
		}
	}
}

func splitDeltaText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// Keep original event boundaries from Codex CLI and avoid arbitrary
	// fixed-length splits, which can create unreadable mid-sentence chunks.
	return []string{text}
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
	normalized := map[string]any{
		"role": "assistant",
		"text": text,
		"done": false,
	}
	a.appendEvent(sessionID, "message.delta", payload, normalized)
}

func (a *Adapter) appendAssistantDone(sessionID, text string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["text"] = text
	normalized := map[string]any{
		"role": "assistant",
		"text": text,
		"done": true,
	}
	a.appendEvent(sessionID, "message.done", payload, normalized)
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
	normalized := map[string]any{
		"role":   "assistant",
		"text":   "",
		"done":   false,
		"action": action,
	}
	a.appendEvent(sessionID, "message.action", payload, normalized)
}

func (a *Adapter) appendAssistantFailure(sessionID string, err error) {
	msg := "continue 执行失败"
	if err != nil {
		var approvalErr approvalRequiredError
		if errors.As(err, &approvalErr) {
			targetThread := strings.TrimSpace(approvalErr.threadID)
			if targetThread == "" {
				targetThread = strings.TrimSpace(a.getSessionThreadID(sessionID))
			}
			if targetThread == "" {
				targetThread = strings.TrimSpace(sessionID)
			}
			msg = "该会话需要人工确认命令，但 Web 端当前不支持确认交互。请在命令行执行 `codex resume " + targetThread + "` 完成确认后再回到 Web，或新建会话继续。"
		} else {
			msg = msg + ": " + err.Error()
		}
	}
	a.appendAssistantDone(sessionID, msg, map[string]any{
		"raw_type": "assistant_error",
		"error":    msg,
		"source":   "cli",
	})
}

func (a *Adapter) appendUserEvent(sessionID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	payload := map[string]any{
		"raw_type": "user_message",
		"text":     text,
	}
	normalized := map[string]any{
		"role": "user",
		"text": text,
		"done": true,
	}
	a.appendEvent(sessionID, "message.user", payload, normalized)
}

func (a *Adapter) appendHistoryMessage(sessionID, role, text string) {
	role = strings.ToLower(strings.TrimSpace(role))
	text = strings.TrimSpace(text)
	if role == "" || text == "" {
		return
	}
	payload := map[string]any{
		"raw_type": "history_message",
		"source":   defaultHistorySource,
		"text":     text,
	}
	normalized := map[string]any{
		"role": role,
		"text": text,
		"done": true,
	}
	eventType := "message.done"
	if role == "user" {
		eventType = "message.user"
	}
	a.appendEvent(sessionID, eventType, payload, normalized)
	if role == "user" {
		a.setSessionStatus(sessionID, sessionStatusRunning)
	} else if role == "assistant" {
		a.setSessionStatus(sessionID, sessionStatusIdle)
	}
}

func (a *Adapter) appendEvent(sessionID, eventType string, payload, normalized map[string]any) {
	now := time.Now().UTC()
	a.mu.Lock()
	s, ok := a.sessions[sessionID]
	if !ok {
		a.mu.Unlock()
		return
	}
	ev := model.SessionEvent{
		Adapter:    adapterName,
		SessionID:  sessionID,
		Seq:        s.nextSeq,
		Ts:         now,
		Type:       eventType,
		Payload:    payload,
		Normalized: normalized,
	}
	s.nextSeq++
	s.events = append(s.events, ev)
	s.detail.UpdatedAt = ev.Ts

	readySubs := make([]*subscriber, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
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

	s, ok := a.sessions[sessionID]
	if !ok {
		return
	}
	next := normalizeSessionStatus(status)
	if next == sessionStatusIdle && s.activeRuns > 0 {
		next = sessionStatusRunning
	}
	s.detail.Status = next
	s.detail.UpdatedAt = time.Now().UTC()
}

func (a *Adapter) finishSessionRun(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sessions[sessionID]
	if !ok {
		return
	}
	if s.activeRuns > 0 {
		s.activeRuns--
	}
	if s.activeRuns > 0 {
		s.detail.Status = sessionStatusRunning
	} else {
		s.detail.Status = sessionStatusIdle
	}
	s.detail.UpdatedAt = time.Now().UTC()
}

func (a *Adapter) getSessionThreadID(sessionID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	s, ok := a.sessions[sessionID]
	if !ok {
		return ""
	}
	if strings.TrimSpace(s.codexThread) != "" {
		return strings.TrimSpace(s.codexThread)
	}
	return inferSessionThreadID(s.detail.ID, s.detail.Metadata)
}

func (a *Adapter) setSessionThreadID(sessionID, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sessions[sessionID]
	if !ok {
		return
	}
	if strings.TrimSpace(s.codexThread) == threadID {
		return
	}
	s.codexThread = threadID
	if s.detail.Metadata == nil {
		s.detail.Metadata = make(map[string]any, 2)
	}
	s.detail.Metadata["codex_thread_id"] = threadID
}

func normalizeSessionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case sessionStatusRunning:
		return sessionStatusRunning
	default:
		return sessionStatusIdle
	}
}

func inferSessionThreadID(sessionID string, metadata map[string]any) string {
	if metadata != nil {
		if tid, _ := metadata["codex_thread_id"].(string); strings.TrimSpace(tid) != "" {
			return strings.TrimSpace(tid)
		}
	}
	if looksLikeThreadID(sessionID) {
		return strings.TrimSpace(sessionID)
	}
	return ""
}

func looksLikeThreadID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts) != 5 {
		return false
	}
	expected := []int{8, 4, 4, 4, 12}
	for idx, part := range parts {
		if len(part) != expected[idx] {
			return false
		}
	}
	return true
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

func loadRuntimeOptions() (streamMode string, cliBin string, cliArgs []string, cliTimeout time.Duration, mockFallback bool) {
	streamMode = strings.ToLower(strings.TrimSpace(os.Getenv("CODEX_STREAM_MODE")))
	if streamMode == "" {
		streamMode = "real"
	}
	if streamMode != "real" && streamMode != "mock" {
		streamMode = "real"
	}

	cliBin = strings.TrimSpace(os.Getenv("CODEX_CLI_BIN"))
	if cliBin == "" {
		cliBin = defaultCLIBin
	}

	argsRaw := strings.TrimSpace(os.Getenv("CODEX_CLI_ARGS"))
	if argsRaw == "" {
		argsRaw = defaultCLIArgs
	}
	cliArgs = strings.Fields(argsRaw)
	if len(cliArgs) == 0 {
		cliArgs = []string{"exec", "--json", cliBypassFlag}
	}
	cliArgs = ensureCLIBypassFlag(cliArgs)

	cliTimeout = defaultCLITimeout
	if raw := strings.TrimSpace(os.Getenv("CODEX_CLI_TIMEOUT_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			cliTimeout = time.Duration(ms) * time.Millisecond
		}
	}

	mockFallback = parseBoolEnv("CODEX_MOCK_FALLBACK", false)
	return streamMode, cliBin, cliArgs, cliTimeout, mockFallback
}

func ensureCLIBypassFlag(args []string) []string {
	for _, arg := range args {
		if strings.TrimSpace(arg) == cliBypassFlag {
			return args
		}
	}
	return append(args, cliBypassFlag)
}

func loadHistoryOptions() (enabled bool, historyDir string, ttl time.Duration) {
	enabled = parseBoolEnv("CODEX_HISTORY_ENABLED", true)
	if !enabled {
		return false, "", defaultHistoryTTL
	}

	historyDir = strings.TrimSpace(os.Getenv("CODEX_HISTORY_DIR"))
	if historyDir == "" {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			historyDir = filepath.Join(home, ".codex", "sessions")
		}
	}

	ttl = defaultHistoryTTL
	if raw := strings.TrimSpace(os.Getenv("CODEX_HISTORY_SCAN_TTL_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			ttl = time.Duration(ms) * time.Millisecond
		}
	}
	return enabled, historyDir, ttl
}

func loadHistoryActiveWindow() time.Duration {
	window := defaultHistoryWindow
	if raw := strings.TrimSpace(os.Getenv("CODEX_HISTORY_ACTIVE_WINDOW_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil {
			if ms <= 0 {
				return 0
			}
			window = time.Duration(ms) * time.Millisecond
		}
	}
	return window
}

func loadHistoryTailInterval() time.Duration {
	interval := defaultHistoryTail
	if raw := strings.TrimSpace(os.Getenv("CODEX_HISTORY_TAIL_INTERVAL_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil {
			if ms <= 0 {
				return 0
			}
			interval = time.Duration(ms) * time.Millisecond
		}
	}
	return interval
}

func isHistoryBackedSession(detail model.SessionDetail) bool {
	if detail.Metadata == nil {
		return false
	}
	if origin, ok := detail.Metadata["origin"].(string); ok {
		if strings.EqualFold(strings.TrimSpace(origin), "codex_history") {
			return true
		}
	}
	if _, ok := detail.Metadata["rollout_path"]; ok {
		return true
	}
	return false
}

func applyHistoryDetail(detail *model.SessionDetail, info externalSessionInfo, keepRunning bool) {
	// Never let stale history status overwrite fresher in-memory status.
	historyIsFresher := !info.detail.UpdatedAt.Before(detail.UpdatedAt)
	if historyIsFresher {
		incoming := normalizeSessionStatus(info.detail.Status)
		current := normalizeSessionStatus(detail.Status)
		if !(keepRunning && current == sessionStatusRunning && incoming == sessionStatusIdle) {
			detail.Status = incoming
		}
	}
	if info.detail.UpdatedAt.After(detail.UpdatedAt) {
		detail.UpdatedAt = info.detail.UpdatedAt
	}
	if detail.Title == "" || detail.Title == detail.ID {
		detail.Title = info.detail.Title
	}
	if detail.Workspace == "" {
		detail.Workspace = info.detail.Workspace
	}
	if detail.Source == "" {
		detail.Source = info.detail.Source
	}
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func lastN(items []string, n int) []string {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) <= n {
		return items
	}
	return items[len(items)-n:]
}

func loadSeedEvents(path string, base time.Time) []model.SessionEvent {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	out := make([]model.SessionEvent, 0, 8)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNo++
		if line == "" {
			continue
		}
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		rawType, _ := payload["type"].(string)
		if rawType == "" {
			rawType = "unknown"
		}

		text := ""
		if item, ok := payload["item"].(map[string]any); ok {
			if v, ok := item["text"].(string); ok {
				text = v
			}
		}
		if text == "" {
			if usage, ok := payload["usage"].(map[string]any); ok {
				text = fmt.Sprintf("usage: input=%v output=%v", usage["input_tokens"], usage["output_tokens"])
			}
		}
		if text == "" {
			text = rawType
		}

		eventType := "message.delta"
		done := false
		if rawType == "turn.completed" {
			eventType = "message.done"
			done = true
		}

		out = append(out, model.SessionEvent{
			Adapter:   adapterName,
			SessionID: defaultSessionID,
			Ts:        base.Add(time.Duration(lineNo) * time.Second),
			Type:      eventType,
			Payload: map[string]any{
				"raw_type": rawType,
				"text":     text,
			},
			Normalized: map[string]any{
				"role": "assistant",
				"text": text,
				"done": done,
			},
		})
	}

	if len(out) == 0 {
		return nil
	}

	last := out[len(out)-1]
	if done, _ := last.Normalized["done"].(bool); !done {
		out = append(out, model.SessionEvent{
			Adapter:   adapterName,
			SessionID: defaultSessionID,
			Ts:        last.Ts.Add(time.Second),
			Type:      "message.done",
			Payload: map[string]any{
				"raw_type": "assistant_done",
			},
			Normalized: map[string]any{
				"role": "assistant",
				"text": "",
				"done": true,
			},
		})
	}

	return out
}
