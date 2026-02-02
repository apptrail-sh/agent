package hooks

import (
	"context"

	"github.com/apptrail-sh/agent/internal/model"
)

type EventPublisher interface {
	Publish(ctx context.Context, update model.WorkloadUpdate) error
}

// HeartbeatPublisher is the interface for publishing heartbeat events
type HeartbeatPublisher interface {
	PublishHeartbeat(ctx context.Context, payload model.ClusterHeartbeatPayload) error
}
