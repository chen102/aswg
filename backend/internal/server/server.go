package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const frontendAppRuntimePatch = `
;(() => {
  const PATCH_MARK = "ASWG Runtime Patch v19";
  if (window.__aswgRuntimePatchApplied === PATCH_MARK) {
    return;
  }
  window.__aswgRuntimePatchApplied = PATCH_MARK;
})();
`

type contextKey string

const requestIDKey contextKey = "request_id"

type Server struct {
	cfg            Config
	registry       *adapter.Registry
	httpSrv        *http.Server
	static         http.Handler
	sessionLimiter *fixedWindowRateLimiter
	metaStore      *sessionMetaStore
}

type apiErrorBody struct {
	Type      string         `json:"type"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type apiEnvelope struct {
	Code      int           `json:"code"`
	Message   string        `json:"message"`
	Data      any           `json:"data"`
	RequestID string        `json:"request_id"`
	Error     *apiErrorBody `json:"error,omitempty"`
}

type wsFrame struct {
	FrameType string    `json:"frame_type"`
	RequestID string    `json:"request_id"`
	Seq       int64     `json:"seq,omitempty"`
	Ts        time.Time `json:"ts"`
	Data      any       `json:"data,omitempty"`
}

type wsConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func New(cfg Config, registry *adapter.Registry) *Server {
	if cfg.FrontendDir == "" {
		cfg.FrontendDir = "frontend/src"
	}
	metaStore, err := newSessionMetaStore(cfg.SessionMetaMapFile)
	if err != nil {
		log.Printf("load session meta map failed: %v", err)
		metaStore, _ = newSessionMetaStore("")
	}
	return &Server{
		cfg:            cfg,
		registry:       registry,
		static:         http.FileServer(http.Dir(cfg.FrontendDir)),
		sessionLimiter: newFixedWindowRateLimiter(time.Second),
		metaStore:      metaStore,
	}
}

func (s *Server) Run(ctx context.Context) error {
	s.httpSrv = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("server started on %s", s.httpSrv.Addr)
	err := s.httpSrv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/adapters", s.handleAdapters)
	mux.HandleFunc("/api/v1/adapters/", s.handleAdapterRoutes)
	mux.HandleFunc("/ws/v1/adapters/", s.handleWSRoutes)
	mux.HandleFunc("/", s.handleFrontend)
	return s.withRequestID(s.withRecover(s.withLogging(mux)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}

	type adapterHealth struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		LatencyMS int64  `json:"latency_ms"`
	}

	requestID := getRequestID(r)
	items := s.registry.List()
	head := make([]adapterHealth, 0, len(items))
	overallStatus := "ok"
	for _, info := range items {
		a, _ := s.registry.Get(info.Name)
		hcCtx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		latency, err := a.HealthCheck(hcCtx)
		cancel()
		status := "ok"
		if err != nil {
			status = "degraded"
			overallStatus = "degraded"
		}
		head = append(head, adapterHealth{Name: info.Name, Status: status, LatencyMS: latency})
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"status":   overallStatus,
			"version":  s.cfg.Version,
			"time":     time.Now().UTC(),
			"adapters": head,
		},
	})
}

func (s *Server) handleAdapters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}
	if !s.requireAuth(w, r, false) {
		return
	}
	requestID := getRequestID(r)
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"items": s.registry.List(),
		},
	})
}

func (s *Server) handleAdapterRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, false) {
		return
	}
	requestID := getRequestID(r)

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/adapters/")
	parts := splitPath(path)
	if len(parts) < 2 {
		s.writeNotFound(w, requestID)
		return
	}

	adapterName := parts[0]
	a, ok := s.registry.Get(adapterName)
	if !ok {
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, map[string]any{"adapter": adapterName})
		return
	}

	if parts[1] != "sessions" {
		s.writeNotFound(w, requestID)
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		s.handleSessionsList(w, r, requestID, a, adapterName)
	case len(parts) == 2 && r.Method == http.MethodPost:
		s.handleCreateSession(w, r, requestID, a)
	case len(parts) == 3 && r.Method == http.MethodGet:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleSessionDetail(w, r, requestID, a, adapterName, sessionID)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleDeleteSession(w, r, requestID, a, adapterName, sessionID)
	case len(parts) == 4 && parts[3] == "events" && r.Method == http.MethodGet:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleSessionEvents(w, r, requestID, a, sessionID)
	case len(parts) == 4 && parts[3] == "continue" && r.Method == http.MethodPost:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleContinue(w, r, requestID, a, sessionID)
	case len(parts) == 4 && parts[3] == "meta" && (r.Method == http.MethodGet || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete):
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleSessionMeta(w, r, requestID, a, adapterName, sessionID)
	default:
		s.writeMethodNotAllowed(w, r)
	}
}

func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, adapterName string) {
	if !s.allowSessionListRate(w, r, requestID) {
		return
	}

	q := r.URL.Query()
	limit, err := parseInt(q.Get("limit"), model.DefaultSessionsLimit)
	if err != nil || limit < 1 || limit > model.MaxSessionsLimit {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: limit", requestID, "validation_error", false, map[string]any{"field": "limit", "reason": "must be between 1 and 100"})
		return
	}

	updatedAfter, err := parseRFC3339Ptr(q.Get("updated_after"))
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: updated_after", requestID, "validation_error", false, map[string]any{"field": "updated_after", "reason": "must be RFC3339"})
		return
	}
	updatedBefore, err := parseRFC3339Ptr(q.Get("updated_before"))
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: updated_before", requestID, "validation_error", false, map[string]any{"field": "updated_before", "reason": "must be RFC3339"})
		return
	}

	resp, err := a.DiscoverSessions(r.Context(), model.DiscoverRequest{
		Query:         q.Get("query"),
		Workspace:     q.Get("workspace"),
		UpdatedAfter:  updatedAfter,
		UpdatedBefore: updatedBefore,
		Limit:         limit,
		Cursor:        q.Get("cursor"),
		SortBy:        q.Get("sort_by"),
		SortOrder:     q.Get("sort_order"),
	})
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	for i := range resp.Items {
		s.applyMetaToSessionSummary(adapterName, &resp.Items[i])
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: resp})
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, adapterName, sessionID string) {
	detail, err := a.GetSession(r.Context(), sessionID)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	s.applyMetaToSessionDetail(adapterName, &detail)
	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: detail})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, adapterName, sessionID string) {
	if err := a.DeleteSession(r.Context(), sessionID); err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	_ = s.metaStore.Delete(adapterName, sessionID)
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"session_id": sessionID,
			"deleted":    true,
		},
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter) {
	defer r.Body.Close()
	var body model.CreateSessionRequest

	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		if !errors.Is(err, io.EOF) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{
				"field":  "body",
				"reason": err.Error(),
			})
			return
		}
	}

	detail, err := a.CreateSession(r.Context(), model.CreateSessionInput{
		Title:      body.Title,
		Workspace:  body.Workspace,
		SeedPrompt: body.SeedPrompt,
	})
	if err != nil {
		if errors.Is(err, model.ErrInvalidParam) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter", requestID, "validation_error", false, nil)
			return
		}
		s.writeMappedError(w, requestID, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data:      detail,
	})
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	q := r.URL.Query()
	limit, err := parseInt(q.Get("limit"), model.DefaultEventsLimit)
	if err != nil || limit < 1 || limit > model.MaxEventsLimit {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: limit", requestID, "validation_error", false, map[string]any{"field": "limit", "reason": "must be between 1 and 500"})
		return
	}

	resp, err := a.GetSessionEvents(r.Context(), model.EventsRequest{
		SessionID: sessionID,
		Limit:     limit,
		Cursor:    q.Get("cursor"),
	})
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: resp})
}

func (s *Server) handleSessionMeta(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, adapterName, sessionID string) {
	if _, err := a.GetSession(r.Context(), sessionID); err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		meta, ok := s.metaStore.Get(adapterName, sessionID)
		if !ok {
			meta = model.SessionMeta{}
		}
		s.writeJSON(w, http.StatusOK, apiEnvelope{
			Code:      0,
			Message:   "ok",
			RequestID: requestID,
			Data: map[string]any{
				"adapter":    adapterName,
				"session_id": sessionID,
				"meta":       meta,
			},
		})
		return
	case http.MethodDelete:
		if err := s.metaStore.Delete(adapterName, sessionID); err != nil {
			s.writeMappedError(w, requestID, err)
			return
		}
		s.writeJSON(w, http.StatusOK, apiEnvelope{
			Code:      0,
			Message:   "ok",
			RequestID: requestID,
			Data: map[string]any{
				"adapter":    adapterName,
				"session_id": sessionID,
				"deleted":    true,
			},
		})
		return
	case http.MethodPut, http.MethodPatch:
		defer r.Body.Close()
		var body model.SessionMetaRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			if !errors.Is(err, io.EOF) {
				s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{
					"field":  "body",
					"reason": err.Error(),
				})
				return
			}
		}

		meta, err := s.metaStore.Upsert(adapterName, sessionID, model.SessionMeta{
			Name: body.Name,
			Note: body.Note,
			Type: body.Type,
		})
		if err != nil {
			s.writeMappedError(w, requestID, err)
			return
		}
		s.writeJSON(w, http.StatusOK, apiEnvelope{
			Code:      0,
			Message:   "ok",
			RequestID: requestID,
			Data: map[string]any{
				"adapter":    adapterName,
				"session_id": sessionID,
				"meta":       meta,
				"deleted":    isEmptySessionMeta(meta),
			},
		})
		return
	default:
		s.writeMethodNotAllowed(w, r)
	}
}

func (s *Server) applyMetaToSessionSummary(adapterName string, item *model.SessionSummary) {
	if item == nil {
		return
	}
	meta, ok := s.metaStore.Get(adapterName, item.ID)
	if !ok {
		item.Meta = nil
		return
	}
	item.Meta = &meta
}

func (s *Server) applyMetaToSessionDetail(adapterName string, detail *model.SessionDetail) {
	if detail == nil {
		return
	}
	meta, ok := s.metaStore.Get(adapterName, detail.ID)
	if !ok {
		detail.Meta = nil
		return
	}
	detail.Meta = &meta
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	defer r.Body.Close()
	var body model.ContinueRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{"field": "body", "reason": err.Error()})
		return
	}

	job, err := a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID:      sessionID,
		Prompt:         body.Prompt,
		Cwd:            body.Cwd,
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		if errors.Is(err, model.ErrSessionNotFound) {
			s.writeBusinessError(w, http.StatusNotFound, 4003, "session not found", requestID, "not_found", false, map[string]any{"session_id": sessionID})
			return
		}
		if errors.Is(err, model.ErrInvalidParam) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: prompt", requestID, "validation_error", false, map[string]any{"field": "prompt", "reason": "must be between 1 and 8000"})
			return
		}
		s.writeBusinessError(w, http.StatusInternalServerError, 4004, "continue start failed", requestID, "adapter_error", true, map[string]any{"reason": err.Error()})
		return
	}

	s.writeJSON(w, http.StatusAccepted, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: job})
}

func (s *Server) allowSessionListRate(w http.ResponseWriter, r *http.Request, requestID string) bool {
	if s.cfg.RateLimitSessionsPerSec <= 0 {
		return true
	}
	key := "sessions:" + clientIP(r)
	if s.sessionLimiter.allow(key, s.cfg.RateLimitSessionsPerSec, time.Now().UTC()) {
		return true
	}
	s.writeBusinessError(w, http.StatusTooManyRequests, 4290, "too many requests", requestID, "rate_limited", true, map[string]any{
		"limit_per_sec": s.cfg.RateLimitSessionsPerSec,
	})
	return false
}

func (s *Server) handleWSRoutes(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestID(r)
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}
	if !s.requireAuth(w, r, true) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/ws/v1/adapters/")
	parts := splitPath(path)
	if len(parts) != 3 || parts[1] != "sessions" {
		s.writeNotFound(w, requestID)
		return
	}

	adapterName := parts[0]
	a, ok := s.registry.Get(adapterName)
	if !ok {
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, map[string]any{"adapter": adapterName})
		return
	}

	sessionID, err := url.PathUnescape(parts[2])
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
		return
	}
	if _, err := a.GetSession(r.Context(), sessionID); err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	fromSeq, err := parseSeqFromQuery(r.URL.Query())
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: cursor/last_seq", requestID, "validation_error", false, map[string]any{"field": "cursor", "reason": err.Error()})
		return
	}

	conn, err := upgradeToWebSocket(w, r)
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "websocket upgrade failed", requestID, "validation_error", false, map[string]any{"reason": err.Error()})
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		buf := make([]byte, 1024)
		for {
			if _, readErr := conn.conn.Read(buf); readErr != nil {
				cancel()
				return
			}
		}
	}()

	eventCh, unsubscribe, err := a.Subscribe(ctx, sessionID, fromSeq)
	if err != nil {
		_ = conn.WriteJSON(wsFrame{FrameType: "error", RequestID: requestID, Ts: time.Now().UTC(), Data: map[string]any{"code": 4003, "message": "session not found"}})
		return
	}
	defer unsubscribe()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	lastSeq := fromSeq
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			lastSeq = ev.Seq
			if err := conn.WriteJSON(wsFrame{
				FrameType: "event",
				RequestID: requestID,
				Seq:       ev.Seq,
				Ts:        time.Now().UTC(),
				Data:      ev,
			}); err != nil {
				return
			}
			if done, _ := ev.Normalized["done"].(bool); done {
				if err := conn.WriteJSON(wsFrame{FrameType: "done", RequestID: requestID, Seq: ev.Seq, Ts: time.Now().UTC(), Data: map[string]any{"session_id": sessionID}}); err != nil {
					return
				}
			}
		case <-ticker.C:
			if err := conn.WriteJSON(wsFrame{FrameType: "heartbeat", RequestID: requestID, Seq: lastSeq, Ts: time.Now().UTC(), Data: map[string]any{"session_id": sessionID}}); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFile(w, r, filepath.Join(s.cfg.FrontendDir, "index.html"))
		return
	}

	candidate := filepath.Clean(filepath.Join(s.cfg.FrontendDir, r.URL.Path))
	if strings.HasSuffix(strings.ToLower(r.URL.Path), ".js") && s.servePatchedJS(w, r, candidate) {
		return
	}
	if _, err := os.Stat(candidate); err == nil {
		http.ServeFile(w, r, candidate)
		return
	}

	http.ServeFile(w, r, filepath.Join(s.cfg.FrontendDir, "index.html"))
}

func (s *Server) servePatchedJS(w http.ResponseWriter, r *http.Request, candidate string) bool {
	src, err := os.ReadFile(candidate)
	if err != nil {
		return false
	}

	content := string(src)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	// Avoid duplicate append when file already embeds a runtime patch marker.
	if strings.Contains(content, "ASWG Runtime Patch v") {
		_, _ = w.Write(src)
		return true
	}

	// Primary app script gets runtime patch appended.
	lowerPath := strings.ToLower(r.URL.Path)
	shouldInject := lowerPath == "/app.js" ||
		(strings.Contains(content, "aswg_runtime_config_v1") && strings.Contains(content, "chat-thread")) ||
		(strings.Contains(content, "function renderChatThread") && strings.Contains(content, "continue-form"))

	_, _ = w.Write(src)
	if shouldInject {
		_, _ = io.WriteString(w, frontendAppRuntimePatch)
	}
	return true
}

func (s *Server) writeMappedError(w http.ResponseWriter, requestID string, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidParam):
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter", requestID, "validation_error", false, nil)
	case errors.Is(err, model.ErrAdapterNotFound):
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, nil)
	case errors.Is(err, model.ErrSessionNotFound):
		s.writeBusinessError(w, http.StatusNotFound, 4003, "session not found", requestID, "not_found", false, nil)
	default:
		s.writeBusinessError(w, http.StatusInternalServerError, 5000, "internal error", requestID, "internal_error", true, map[string]any{"reason": err.Error()})
	}
}

func (s *Server) writeNotFound(w http.ResponseWriter, requestID string) {
	s.writeBusinessError(w, http.StatusNotFound, 5000, "not found", requestID, "not_found", false, nil)
}

func (s *Server) writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestID(r)
	s.writeBusinessError(w, http.StatusMethodNotAllowed, 4001, "method not allowed", requestID, "validation_error", false, nil)
}

func (s *Server) writeBusinessError(w http.ResponseWriter, httpStatus, code int, msg, requestID, typ string, retryable bool, details map[string]any) {
	s.writeJSON(w, httpStatus, apiEnvelope{
		Code:      code,
		Message:   msg,
		Data:      nil,
		RequestID: requestID,
		Error: &apiErrorBody{
			Type:      typ,
			Retryable: retryable,
			Details:   details,
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v apiEnvelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request, allowQueryToken bool) bool {
	if s.cfg.AuthToken == "" {
		return true
	}
	token := extractBearerToken(r.Header.Get("Authorization"))
	if token == "" && allowQueryToken {
		token = strings.TrimSpace(r.URL.Query().Get("access_token"))
	}
	if token != s.cfg.AuthToken {
		s.writeBusinessError(w, http.StatusUnauthorized, 4010, "unauthorized", getRequestID(r), "auth_error", false, nil)
		return false
	}
	return true
}

func extractBearerToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func parseInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func parseRFC3339Ptr(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	ut := t.UTC()
	return &ut, nil
}

func parseSeqFromQuery(values url.Values) (int64, error) {
	if cursor := strings.TrimSpace(values.Get("cursor")); cursor != "" {
		return model.DecodeSeqCursor(cursor)
	}
	if rawLast := strings.TrimSpace(values.Get("last_seq")); rawLast != "" {
		v, err := strconv.ParseInt(rawLast, 10, 64)
		if err != nil {
			return 0, err
		}
		if v < 0 {
			return 0, fmt.Errorf("last_seq must be >= 0")
		}
		return v, nil
	}
	return 0, nil
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-Id")
		if strings.TrimSpace(reqID) == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-Id", reqID)
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered request_id=%s err=%v", getRequestID(r), rec)
				s.writeBusinessError(w, http.StatusInternalServerError, 5000, "internal error", getRequestID(r), "internal_error", true, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("request_id=%s method=%s path=%s latency_ms=%d", getRequestID(r), r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

func getRequestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey).(string); ok && v != "" {
		return v
	}
	return "req_unknown"
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(buf)
}

func upgradeToWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return nil, fmt.Errorf("missing Connection: upgrade")
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return nil, fmt.Errorf("missing Upgrade: websocket")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("server does not support websocket")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	hasher := sha1.New() // #nosec G401: protocol requires SHA-1 for websocket handshake.
	_, _ = hasher.Write([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

	response := []string{
		"HTTP/1.1 101 Switching Protocols",
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Accept: " + accept,
		"",
		"",
	}
	if _, err := rw.WriteString(strings.Join(response, "\r\n")); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn}, nil
}

func headerContainsToken(h http.Header, key, want string) bool {
	vals := h.Values(key)
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}

func (c *wsConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode)
	l := len(payload)
	switch {
	case l < 126:
		header = append(header, byte(l))
	case l <= 65535:
		header = append(header, 126)
		tmp := make([]byte, 2)
		binary.BigEndian.PutUint16(tmp, uint16(l))
		header = append(header, tmp...)
	default:
		header = append(header, 127)
		tmp := make([]byte, 8)
		binary.BigEndian.PutUint64(tmp, uint64(l))
		header = append(header, tmp...)
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func (c *wsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func ensureFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findFrontendDir(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if ensureFile(filepath.Join(p, "index.html")) {
			return p
		}
	}
	return "frontend/src"
}

func ResolveFrontendDir(configured string) string {
	return findFrontendDir(
		configured,
		"frontend/src",
		"../frontend/src",
	)
}

func upgradeReader(conn net.Conn) *bufio.Reader {
	return bufio.NewReader(conn)
}
