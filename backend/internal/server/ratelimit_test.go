package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/adapter/codex"
)

func TestSessionsRouteReturns4290WhenRateLimited(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	registry := adapter.NewRegistry("codex")
	codexAdapter, err := codex.NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	registry.Register(codexAdapter)

	srv := New(Config{
		AuthToken:               "",
		DefaultAdapter:          "codex",
		Version:                 "test",
		RateLimitSessionsPerSec: 5,
		FrontendDir:             "../../frontend/src",
	}, registry)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := ts.Client()
	hit429 := false
	for i := 0; i < 30; i++ {
		resp, err := client.Get(ts.URL + "/api/v1/adapters/codex/sessions?limit=1")
		if err != nil {
			t.Fatalf("GET sessions error = %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusTooManyRequests {
			continue
		}
		hit429 = true

		var envelope struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("unmarshal body error = %v body=%s", err, string(body))
		}
		if envelope.Code != 4290 {
			t.Fatalf("expected business code 4290, got %d body=%s", envelope.Code, string(body))
		}
		break
	}

	if !hit429 {
		t.Fatalf("expected at least one 429 response within burst requests")
	}
}
