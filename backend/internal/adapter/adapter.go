package adapter

import (
	"context"

	"agent-session-web-gateway/backend/internal/model"
)

type AgentAdapter interface {
	Name() string
	DisplayName() string
	Version() string
	Capabilities() []string
	CreateSession(ctx context.Context, req model.CreateSessionInput) (model.SessionDetail, error)
	DeleteSession(ctx context.Context, sessionID string) error
	DiscoverSessions(ctx context.Context, req model.DiscoverRequest) (model.PagedSessions, error)
	GetSession(ctx context.Context, sessionID string) (model.SessionDetail, error)
	GetSessionEvents(ctx context.Context, req model.EventsRequest) (model.PagedEvents, error)
	ContinueSession(ctx context.Context, req model.ContinueInput) (model.RunJob, error)
	Subscribe(ctx context.Context, sessionID string, fromSeq int64) (<-chan model.SessionEvent, func(), error)
	HealthCheck(ctx context.Context) (int64, error)
}
