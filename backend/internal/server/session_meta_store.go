package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-session-web-gateway/backend/internal/model"
)

type sessionMetaStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]sessionMetaEntry
}

type sessionMetaEntry struct {
	Adapter   string            `json:"adapter"`
	SessionID string            `json:"session_id"`
	Meta      model.SessionMeta `json:"meta"`
}

type sessionMetaTable struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Items     []sessionMetaEntry `json:"items"`
}

func newSessionMetaStore(path string) (*sessionMetaStore, error) {
	store := &sessionMetaStore{
		path:  strings.TrimSpace(path),
		items: make(map[string]sessionMetaEntry),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *sessionMetaStore) Get(adapter, sessionID string) (model.SessionMeta, bool) {
	key, ok := sessionMetaKey(adapter, sessionID)
	if !ok {
		return model.SessionMeta{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, exists := s.items[key]
	if !exists {
		return model.SessionMeta{}, false
	}
	return entry.Meta, true
}

func (s *sessionMetaStore) Upsert(adapter, sessionID string, meta model.SessionMeta) (model.SessionMeta, error) {
	key, ok := sessionMetaKey(adapter, sessionID)
	if !ok {
		return model.SessionMeta{}, model.ErrInvalidParam
	}

	normalized := normalizeSessionMeta(meta)
	normalized.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	if isEmptySessionMeta(normalized) {
		delete(s.items, key)
		err := s.persistLocked()
		s.mu.Unlock()
		return model.SessionMeta{}, err
	}
	s.items[key] = sessionMetaEntry{
		Adapter:   strings.TrimSpace(adapter),
		SessionID: strings.TrimSpace(sessionID),
		Meta:      normalized,
	}
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return model.SessionMeta{}, err
	}
	return normalized, nil
}

func (s *sessionMetaStore) Delete(adapter, sessionID string) error {
	key, ok := sessionMetaKey(adapter, sessionID)
	if !ok {
		return nil
	}
	s.mu.Lock()
	delete(s.items, key)
	err := s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *sessionMetaStore) load() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var table sessionMetaTable
	if err := json.Unmarshal(content, &table); err != nil {
		return err
	}
	for _, item := range table.Items {
		key, ok := sessionMetaKey(item.Adapter, item.SessionID)
		if !ok {
			continue
		}
		meta := normalizeSessionMeta(item.Meta)
		if item.Meta.UpdatedAt.IsZero() {
			meta.UpdatedAt = table.UpdatedAt
		} else {
			meta.UpdatedAt = item.Meta.UpdatedAt
		}
		if isEmptySessionMeta(meta) {
			continue
		}
		s.items[key] = sessionMetaEntry{
			Adapter:   strings.TrimSpace(item.Adapter),
			SessionID: strings.TrimSpace(item.SessionID),
			Meta:      meta,
		}
	}
	return nil
}

func (s *sessionMetaStore) persistLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}

	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	items := make([]sessionMetaEntry, 0, len(s.items))
	for _, entry := range s.items {
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Adapter == items[j].Adapter {
			return items[i].SessionID < items[j].SessionID
		}
		return items[i].Adapter < items[j].Adapter
	})

	table := sessionMetaTable{
		UpdatedAt: time.Now().UTC(),
		Items:     items,
	}

	raw, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "session-meta-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func sessionMetaKey(adapter, sessionID string) (string, bool) {
	adapter = strings.TrimSpace(adapter)
	sessionID = strings.TrimSpace(sessionID)
	if adapter == "" || sessionID == "" {
		return "", false
	}
	return adapter + "::" + sessionID, true
}

func normalizeSessionMeta(meta model.SessionMeta) model.SessionMeta {
	meta.Name = trimRunes(strings.TrimSpace(meta.Name), 240)
	meta.Note = trimRunes(strings.TrimSpace(meta.Note), 2000)
	meta.Type = trimRunes(strings.TrimSpace(meta.Type), 120)
	return meta
}

func isEmptySessionMeta(meta model.SessionMeta) bool {
	return strings.TrimSpace(meta.Name) == "" &&
		strings.TrimSpace(meta.Note) == "" &&
		strings.TrimSpace(meta.Type) == ""
}

func trimRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
