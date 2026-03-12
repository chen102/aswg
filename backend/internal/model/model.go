package model

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSessionsLimit = 20
	MaxSessionsLimit     = 100
	DefaultEventsLimit   = 100
	MaxEventsLimit       = 500
)

var (
	ErrAdapterNotFound = fmt.Errorf("adapter not found")
	ErrSessionNotFound = fmt.Errorf("session not found")
	ErrInvalidParam    = fmt.Errorf("invalid parameter")
)

type AdapterInfo struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Enabled      bool     `json:"enabled"`
	Default      bool     `json:"default"`
	Capabilities []string `json:"capabilities"`
	Version      string   `json:"version"`
}

type SessionSummary struct {
	Adapter   string       `json:"adapter"`
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Status    string       `json:"status"`
	UpdatedAt time.Time    `json:"updated_at"`
	Workspace string       `json:"workspace"`
	Source    string       `json:"source"`
	Meta      *SessionMeta `json:"session_meta,omitempty"`
}

type SessionDetail struct {
	Adapter   string         `json:"adapter"`
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Workspace string         `json:"workspace"`
	Source    string         `json:"source"`
	Metadata  map[string]any `json:"metadata"`
	Meta      *SessionMeta   `json:"session_meta,omitempty"`
}

type SessionMeta struct {
	Name      string    `json:"name,omitempty"`
	Note      string    `json:"note,omitempty"`
	Type      string    `json:"type,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type SessionEvent struct {
	Adapter    string         `json:"adapter"`
	SessionID  string         `json:"session_id"`
	Seq        int64          `json:"seq"`
	Ts         time.Time      `json:"ts"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload"`
	Normalized map[string]any `json:"normalized"`
}

type ContinueRequest struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd,omitempty"`
}

type CreateSessionRequest struct {
	Title      string `json:"title,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
	SeedPrompt string `json:"seed_prompt,omitempty"`
}

type CreateSessionInput struct {
	Title      string
	Workspace  string
	SeedPrompt string
}

type SessionMetaRequest struct {
	Name string `json:"name,omitempty"`
	Note string `json:"note,omitempty"`
	Type string `json:"type,omitempty"`
}

type ContinueInput struct {
	SessionID      string
	Prompt         string
	Cwd            string
	IdempotencyKey string
}

type RunJob struct {
	JobID     string    `json:"job_id"`
	Adapter   string    `json:"adapter"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
}

type DiscoverRequest struct {
	Query         string
	Workspace     string
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
	Limit         int
	Cursor        string
	SortBy        string
	SortOrder     string
}

type EventsRequest struct {
	SessionID string
	Limit     int
	Cursor    string
}

type PagedSessions struct {
	Items      []SessionSummary `json:"items"`
	NextCursor string           `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
}

type PagedEvents struct {
	Items      []SessionEvent `json:"items"`
	NextCursor string         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

func EncodeIndexCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("idx:%d", offset)))
}

func DecodeIndexCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[0] != "idx" {
		return 0, fmt.Errorf("invalid index cursor")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	if offset < 0 {
		return 0, fmt.Errorf("index cursor must be >= 0")
	}
	return offset, nil
}

func EncodeSeqCursor(seq int64) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("seq:%d", seq)))
}

func DecodeSeqCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[0] != "seq" {
		return 0, fmt.Errorf("invalid seq cursor")
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, err
	}
	if seq < 0 {
		return 0, fmt.Errorf("seq cursor must be >= 0")
	}
	return seq, nil
}
