package hooks

import (
	"context"

	"github.com/apptrail-sh/agent/internal/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type EventPublisherQueue struct {
	UpdateChan <-chan model.WorkloadUpdate
	publishers []EventPublisher
}

func NewEventPublisherQueue(updateChan <-chan model.WorkloadUpdate, publishers []EventPublisher) *EventPublisherQueue {
	return &EventPublisherQueue{
		UpdateChan: updateChan,
		publishers: publishers,
	}
}

func (eq *EventPublisherQueue) Loop() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	logger.Info("Event publisher queue started", "publishers", len(eq.publishers))

	for update := range eq.UpdateChan {
		logger.Info("Received workload update",
			"namespace", update.Namespace,
			"name", update.Name,
			"kind", update.Kind,
			"previousVersion", update.PreviousVersion,
			"currentVersion", update.CurrentVersion,
		)

		// Publish to all registered publishers
		for _, publisher := range eq.publishers {
			// Publish all version updates, including initial deployments (where PreviousVersion is empty)
			err := publisher.Publish(ctx, update)
			if err != nil {
				logger.Error(err, "failed to publish event",
					"namespace", update.Namespace,
					"name", update.Name,
				)
			}
		}
	}
}
