package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-session-web-gateway/backend/internal/model"
)

func TestContinueSessionRespectsContextCancel(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	lastSeq := mustLatestSeq(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := a.ContinueSession(ctx, model.ContinueInput{
		SessionID: defaultSessionID,
		Prompt:    "cancel test",
	}); err != nil {
		t.Fatalf("ContinueSession() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(500 * time.Millisecond)

	page, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: defaultSessionID,
		Limit:     model.MaxEventsLimit,
		Cursor:    model.EncodeSeqCursor(lastSeq),
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("expected at least one delta event before cancel")
	}

	for _, ev := range page.Items {
		if ev.Type == "message.done" {
			t.Fatalf("unexpected done event after cancel: seq=%d", ev.Seq)
		}
	}
}

func TestContinueSessionIdempotencyKeyDeduplicates(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	lastSeq := mustLatestSeq(t, a)
	key := "same-idempotency-key"

	job1, err := a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID:      defaultSessionID,
		Prompt:         "idempotency test",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("first ContinueSession() error = %v", err)
	}

	job2, err := a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID:      defaultSessionID,
		Prompt:         "idempotency test",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("second ContinueSession() error = %v", err)
	}

	if job1.JobID != job2.JobID {
		t.Fatalf("expected same job_id for duplicated idempotency key, got %s vs %s", job1.JobID, job2.JobID)
	}

	time.Sleep(700 * time.Millisecond)

	page, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: defaultSessionID,
		Limit:     model.MaxEventsLimit,
		Cursor:    model.EncodeSeqCursor(lastSeq),
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}

	if got, want := len(page.Items), 5; got != want {
		t.Fatalf("expected %d events for deduped continue, got %d", want, got)
	}

	doneCount := 0
	userCount := 0
	for _, ev := range page.Items {
		if ev.Type == "message.done" {
			doneCount++
		}
		if role, _ := ev.Normalized["role"].(string); role == "user" {
			userCount++
		}
	}
	if doneCount != 1 {
		t.Fatalf("expected one done event, got %d", doneCount)
	}
	if userCount != 1 {
		t.Fatalf("expected one user event, got %d", userCount)
	}
}

func TestContinueSessionUpdatesSessionStatus(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	before, err := a.GetSession(context.Background(), defaultSessionID)
	if err != nil {
		t.Fatalf("GetSession() before continue error = %v", err)
	}
	if before.Status != sessionStatusIdle {
		t.Fatalf("expected initial status=%q, got %q", sessionStatusIdle, before.Status)
	}

	if _, err := a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID: defaultSessionID,
		Prompt:    "status transition test",
	}); err != nil {
		t.Fatalf("ContinueSession() error = %v", err)
	}

	during, err := a.GetSession(context.Background(), defaultSessionID)
	if err != nil {
		t.Fatalf("GetSession() during continue error = %v", err)
	}
	if during.Status != sessionStatusRunning {
		t.Fatalf("expected during status=%q, got %q", sessionStatusRunning, during.Status)
	}

	time.Sleep(700 * time.Millisecond)

	after, err := a.GetSession(context.Background(), defaultSessionID)
	if err != nil {
		t.Fatalf("GetSession() after continue error = %v", err)
	}
	if after.Status != sessionStatusIdle {
		t.Fatalf("expected final status=%q, got %q", sessionStatusIdle, after.Status)
	}
}

func TestCreateSessionWithSeedPrompt(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{
		Title:      "Chat P1",
		Workspace:  "/workspace/p1",
		SeedPrompt: "这是第一条用户种子消息",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if detail.ID == "" {
		t.Fatalf("expected session id to be generated")
	}
	if detail.Title != "Chat P1" {
		t.Fatalf("unexpected title: %s", detail.Title)
	}

	events, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: detail.ID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(events.Items) != 2 {
		t.Fatalf("expected 2 seed events, got %d", len(events.Items))
	}
	if role, _ := events.Items[0].Normalized["role"].(string); role != "user" {
		t.Fatalf("expected first event role=user, got %q", role)
	}
}

func TestDeleteSessionRemovesFromDetailAndList(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.CreateSession(context.Background(), model.CreateSessionInput{
		Title:     "To be deleted",
		Workspace: "/workspace/delete",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := a.DeleteSession(context.Background(), detail.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if _, err := a.GetSession(context.Background(), detail.ID); err == nil {
		t.Fatalf("expected deleted session to be missing in GetSession")
	}

	page, err := a.DiscoverSessions(context.Background(), model.DiscoverRequest{
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	for _, item := range page.Items {
		if item.ID == detail.ID {
			t.Fatalf("deleted session %s still visible in discover list", detail.ID)
		}
	}
}

func TestDeleteHistorySessionHidesFromDiscover(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	historyDir := t.TempDir()
	sessionID := "01900000-0000-7000-a100-000000000001"
	writeHistoryRolloutFile(t, historyDir, sessionID)
	t.Setenv("CODEX_HISTORY_DIR", historyDir)

	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if err := a.DeleteSession(context.Background(), sessionID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	page, err := a.DiscoverSessions(context.Background(), model.DiscoverRequest{
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	for _, item := range page.Items {
		if item.ID == sessionID {
			t.Fatalf("deleted history session %s still visible in discover list", sessionID)
		}
	}
}

func TestDiscoverSessionsIncludesHistorySessions(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	historyDir := t.TempDir()
	sessionID := "01900000-0000-7000-a100-000000000001"
	writeHistoryRolloutFile(t, historyDir, sessionID)
	t.Setenv("CODEX_HISTORY_DIR", historyDir)

	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	page, err := a.DiscoverSessions(context.Background(), model.DiscoverRequest{
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}

	found := false
	for _, item := range page.Items {
		if item.ID != sessionID {
			continue
		}
		found = true
		if item.Workspace != "/workspace/aswg" {
			t.Fatalf("unexpected workspace: %s", item.Workspace)
		}
	}
	if !found {
		t.Fatalf("expected historical session %s in discover result", sessionID)
	}
}

func TestGetSessionEventsLoadsHistorySession(t *testing.T) {
	t.Setenv("CODEX_STREAM_MODE", "mock")
	historyDir := t.TempDir()
	sessionID := "01900000-0000-7000-a100-000000000002"
	writeHistoryRolloutFile(t, historyDir, sessionID)
	t.Setenv("CODEX_HISTORY_DIR", historyDir)

	a, err := NewAdapter("")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	detail, err := a.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if detail.ID != sessionID {
		t.Fatalf("unexpected detail id: %s", detail.ID)
	}

	events, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: sessionID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(events.Items) != 2 {
		t.Fatalf("expected 2 historical message events, got %d", len(events.Items))
	}
	if role, _ := events.Items[0].Normalized["role"].(string); role != "user" {
		t.Fatalf("expected first event role=user, got %q", role)
	}
	if role, _ := events.Items[1].Normalized["role"].(string); role != "assistant" {
		t.Fatalf("expected second event role=assistant, got %q", role)
	}
}

func TestSummarizeCLIAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType string
		rawLine   string
		want      string
	}{
		{
			name:      "thread started",
			eventType: "thread.started",
			rawLine:   `{"type":"thread.started"}`,
			want:      "会话线程已启动",
		},
		{
			name:      "turn completed",
			eventType: "turn.completed",
			rawLine:   `{"type":"turn.completed","usage":{"output_tokens":42}}`,
			want:      "本轮请求处理完成",
		},
		{
			name:      "command started",
			eventType: "item.started",
			rawLine:   `{"type":"item.started","item":{"type":"command_execution","command":"/bin/bash -lc 'ls -la'"}}`,
			want:      "正在执行命令: /bin/bash -lc 'ls -la'",
		},
		{
			name:      "command completed",
			eventType: "item.completed",
			rawLine:   `{"type":"item.completed","item":{"type":"command_execution","command":"/bin/bash -lc 'ls -la'","exit_code":0}}`,
			want:      "命令执行完成 (exit 0): /bin/bash -lc 'ls -la'",
		},
		{
			name:      "tool call completed",
			eventType: "item.completed",
			rawLine:   `{"type":"item.completed","item":{"type":"tool_call","name":"mcp__playwright__browser_snapshot"}}`,
			want:      "工具调用完成: mcp__playwright__browser_snapshot",
		},
		{
			name:      "agent message completed",
			eventType: "item.completed",
			rawLine:   `{"type":"item.completed","item":{"type":"agent_message","text":"你好"}}`,
			want:      "回复片段已生成",
		},
		{
			name:      "error event",
			eventType: "turn.error",
			rawLine:   `{"type":"turn.error","message":"failed"}`,
			want:      "执行出现异常",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := summarizeCLIAction(tc.eventType, tc.rawLine)
			if got != tc.want {
				t.Fatalf("summarizeCLIAction() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildCLICommandArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     []string
		prompt   string
		threadID string
		want     []string
	}{
		{
			name:   "first turn uses exec",
			base:   []string{"exec", "--json"},
			prompt: "hello",
			want:   []string{"exec", "--json", "hello"},
		},
		{
			name:     "resume uses thread id",
			base:     []string{"exec", "--json"},
			prompt:   "next",
			threadID: "01900000-0000-7000-a100-000000000003",
			want: []string{
				"exec", "resume", "--json",
				"01900000-0000-7000-a100-000000000003",
				"next",
			},
		},
		{
			name:     "adds exec when missing",
			base:     []string{"--json"},
			prompt:   "next",
			threadID: "01900000-0000-7000-a100-000000000003",
			want: []string{
				"exec", "resume", "--json",
				"01900000-0000-7000-a100-000000000003",
				"next",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildCLICommandArgs(tc.base, tc.prompt, tc.threadID)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("buildCLICommandArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadRuntimeOptionsAlwaysIncludesBypassFlag(t *testing.T) {
	t.Setenv("CODEX_CLI_ARGS", "exec --json")
	_, _, cliArgs, _, _ := loadRuntimeOptions()
	if !containsArg(cliArgs, cliBypassFlag) {
		t.Fatalf("expected cli args to contain %q, got %v", cliBypassFlag, cliArgs)
	}

	t.Setenv("CODEX_CLI_ARGS", "exec --json --model gpt-5")
	_, _, cliArgs, _, _ = loadRuntimeOptions()
	if !containsArg(cliArgs, cliBypassFlag) {
		t.Fatalf("expected cli args to contain %q when customized, got %v", cliBypassFlag, cliArgs)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == want {
			return true
		}
	}
	return false
}

func TestParseCLIThreadID(t *testing.T) {
	t.Parallel()

	eventType := "thread.started"
	rawLine := `{"type":"thread.started","thread_id":"01900000-0000-7000-a100-000000000003"}`
	got := parseCLIThreadID(eventType, rawLine)
	if got != "01900000-0000-7000-a100-000000000003" {
		t.Fatalf("parseCLIThreadID() = %q", got)
	}

	if got := parseCLIThreadID("turn.started", rawLine); got != "" {
		t.Fatalf("parseCLIThreadID() unexpected non-empty: %q", got)
	}
}

func TestInferSessionThreadID(t *testing.T) {
	t.Parallel()

	if got := inferSessionThreadID("01900000-0000-7000-a100-000000000003", nil); got == "" {
		t.Fatalf("expected inferred thread id from session id")
	}

	meta := map[string]any{"codex_thread_id": "01900000-0000-7000-a100-000000000003"}
	if got := inferSessionThreadID("sess_demo_001", meta); got != "01900000-0000-7000-a100-000000000003" {
		t.Fatalf("expected metadata thread id, got %q", got)
	}
}

func mustLatestSeq(t *testing.T, a *Adapter) int64 {
	t.Helper()
	page, err := a.GetSessionEvents(context.Background(), model.EventsRequest{
		SessionID: defaultSessionID,
		Limit:     model.MaxEventsLimit,
	})
	if err != nil {
		t.Fatalf("GetSessionEvents() error = %v", err)
	}
	if len(page.Items) == 0 {
		return 0
	}
	return page.Items[len(page.Items)-1].Seq
}

func writeHistoryRolloutFile(t *testing.T, root, sessionID string) string {
	t.Helper()
	dir := filepath.Join(root, "2026", "03", "07")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path := filepath.Join(dir, "rollout-2026-03-07T18-31-52-"+sessionID+".jsonl")
	lines := []string{
		fmt.Sprintf(`{"timestamp":"2026-03-07T10:33:07.390Z","type":"session_meta","payload":{"id":%q,"timestamp":"2026-03-07T10:33:07.390Z","cwd":"/workspace/aswg","source":"cli"}}`, sessionID),
		`{"timestamp":"2026-03-07T10:33:20.001Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"历史会话测试问题"}]}}`,
		`{"timestamp":"2026-03-07T10:33:25.001Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"历史会话测试回答"}]}}`,
	}

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
