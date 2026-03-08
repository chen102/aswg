package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/adapter/codex"
)

func TestCreateSessionEndpointSuccess(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	registry := adapter.NewRegistry("codex")
	codexAdapter, err := codex.NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	registry.Register(codexAdapter)

	srv := New(Config{
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 30,
		FrontendDir:             "../frontend/src",
	}, registry)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]any{
		"title":       "Created from API",
		"workspace":   "/workspace/api",
		"seed_prompt": "hello seed",
	}
	body, _ := json.Marshal(payload)
	resp, err := ts.Client().Post(ts.URL+"/api/v1/adapters/codex/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST create session error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("expected code=0, got %d", envelope.Code)
	}
	if envelope.Data.ID == "" {
		t.Fatalf("expected non-empty session id")
	}
	if envelope.Data.Title != "Created from API" {
		t.Fatalf("unexpected title: %s", envelope.Data.Title)
	}
}

func TestCreateSessionEndpointInvalidParam(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	registry := adapter.NewRegistry("codex")
	codexAdapter, err := codex.NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	registry.Register(codexAdapter)

	srv := New(Config{
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 30,
		FrontendDir:             "../frontend/src",
	}, registry)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]any{
		"title": strings.Repeat("x", 300),
	}
	body, _ := json.Marshal(payload)
	resp, err := ts.Client().Post(ts.URL+"/api/v1/adapters/codex/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST create session error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if envelope.Code != 4001 {
		t.Fatalf("expected code=4001, got %d", envelope.Code)
	}
}
