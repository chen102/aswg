package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/adapter/codex"
	"agent-session-web-gateway/backend/internal/model"
)

func TestDeleteSessionEndpointSuccess(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	registry := adapter.NewRegistry("codex")
	codexAdapter, err := codex.NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	registry.Register(codexAdapter)

	created, err := codexAdapter.CreateSession(context.Background(), model.CreateSessionInput{
		Title:     "Delete Me",
		Workspace: "/workspace/delete-me",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	srv := New(Config{
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 30,
		FrontendDir:             "../frontend/src",
	}, registry)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/adapters/codex/sessions/"+created.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE session error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			SessionID string `json:"session_id"`
			Deleted   bool   `json:"deleted"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("expected code=0, got %d", envelope.Code)
	}
	if envelope.Data.SessionID != created.ID || !envelope.Data.Deleted {
		t.Fatalf("unexpected delete response data: %+v", envelope.Data)
	}

	getResp, err := ts.Client().Get(ts.URL + "/api/v1/adapters/codex/sessions/" + created.ID)
	if err != nil {
		t.Fatalf("GET deleted session error = %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestDeleteSessionEndpointNotFound(t *testing.T) {
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

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/adapters/codex/sessions/sess_not_found", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE session error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if envelope.Code != 4003 {
		t.Fatalf("expected code=4003, got %d", envelope.Code)
	}
}
