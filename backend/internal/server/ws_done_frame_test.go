package server

import (
	"testing"

	"agent-session-web-gateway/backend/internal/model"
)

func TestShouldEmitDoneFrame(t *testing.T) {
	tests := []struct {
		name string
		ev   model.SessionEvent
		want bool
	}{
		{
			name: "assistant message done",
			ev: model.SessionEvent{
				Type: "message.done",
				Normalized: map[string]any{
					"role": "assistant",
					"done": true,
				},
			},
			want: true,
		},
		{
			name: "assistant message done case-insensitive",
			ev: model.SessionEvent{
				Type: "MESSAGE.DONE",
				Normalized: map[string]any{
					"role": "Assistant",
					"done": true,
				},
			},
			want: true,
		},
		{
			name: "legacy done without role",
			ev: model.SessionEvent{
				Type: "message.done",
				Normalized: map[string]any{
					"done": true,
				},
			},
			want: true,
		},
		{
			name: "user message should not emit done frame",
			ev: model.SessionEvent{
				Type: "message.user",
				Normalized: map[string]any{
					"role": "user",
					"done": true,
				},
			},
			want: false,
		},
		{
			name: "message done but user role should not emit",
			ev: model.SessionEvent{
				Type: "message.done",
				Normalized: map[string]any{
					"role": "user",
					"done": true,
				},
			},
			want: false,
		},
		{
			name: "message done false should not emit",
			ev: model.SessionEvent{
				Type: "message.done",
				Normalized: map[string]any{
					"role": "assistant",
					"done": false,
				},
			},
			want: false,
		},
		{
			name: "non-message done should not emit",
			ev: model.SessionEvent{
				Type: "message.delta",
				Normalized: map[string]any{
					"role": "assistant",
					"done": true,
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEmitDoneFrame(tc.ev)
			if got != tc.want {
				t.Fatalf("shouldEmitDoneFrame() = %v, want %v", got, tc.want)
			}
		})
	}
}

