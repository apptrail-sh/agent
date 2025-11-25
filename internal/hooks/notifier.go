package hooks

import (
	"context"

	"github.com/apptrail-sh/agent/internal/model"
)

type EventPublisher interface {
	Publish(ctx context.Context, update model.WorkloadUpdate) error
}
