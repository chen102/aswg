package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/adapter/codex"
	"agent-session-web-gateway/backend/internal/model"
)

func TestSessionMetaEndpointAndSessionPayloads(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	registry := adapter.NewRegistry("codex")
	codexAdapter, err := codex.NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	registry.Register(codexAdapter)

	created, err := codexAdapter.CreateSession(context.Background(), model.CreateSessionInput{
		Title:     "Meta Test Session",
		Workspace: "/workspace/meta",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	srv := New(Config{
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 30,
		FrontendDir:             "../frontend/src",
		SessionMetaMapFile:      filepath.Join(t.TempDir(), "session-meta-map.json"),
	}, registry)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]string{
		"name": "需求追踪会话",
		"note": "这里记录需求拆解与风险",
		"type": "需求",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/adapters/codex/sessions/"+created.ID+"/meta", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT session meta error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var putEnvelope struct {
		Code int `json:"code"`
		Data struct {
			Meta model.SessionMeta `json:"meta"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&putEnvelope); err != nil {
		t.Fatalf("decode put response error = %v", err)
	}
	if putEnvelope.Code != 0 {
		t.Fatalf("expected code=0, got %d", putEnvelope.Code)
	}
	if putEnvelope.Data.Meta.Name != payload["name"] || putEnvelope.Data.Meta.Type != payload["type"] {
		t.Fatalf("unexpected meta in put response: %+v", putEnvelope.Data.Meta)
	}

	detailResp, err := ts.Client().Get(ts.URL + "/api/v1/adapters/codex/sessions/" + created.ID)
	if err != nil {
		t.Fatalf("GET session detail error = %v", err)
	}
	defer detailResp.Body.Close()
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("expected detail status 200, got %d", detailResp.StatusCode)
	}
	var detailEnvelope struct {
		Code int `json:"code"`
		Data struct {
			ID   string             `json:"id"`
			Meta *model.SessionMeta `json:"session_meta"`
		} `json:"data"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detailEnvelope); err != nil {
		t.Fatalf("decode detail response error = %v", err)
	}
	if detailEnvelope.Data.Meta == nil || detailEnvelope.Data.Meta.Name != payload["name"] {
		t.Fatalf("expected detail to include session_meta name, got %+v", detailEnvelope.Data.Meta)
	}

	listResp, err := ts.Client().Get(ts.URL + "/api/v1/adapters/codex/sessions?limit=100")
	if err != nil {
		t.Fatalf("GET sessions list error = %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.StatusCode)
	}

	var listEnvelope struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				ID   string             `json:"id"`
				Meta *model.SessionMeta `json:"session_meta"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listEnvelope); err != nil {
		t.Fatalf("decode list response error = %v", err)
	}

	found := false
	for _, item := range listEnvelope.Data.Items {
		if item.ID != created.ID {
			continue
		}
		found = true
		if item.Meta == nil || item.Meta.Type != payload["type"] {
			t.Fatalf("expected list item to include session_meta type, got %+v", item.Meta)
		}
	}
	if !found {
		t.Fatalf("created session %s not found in list", created.ID)
	}
}
