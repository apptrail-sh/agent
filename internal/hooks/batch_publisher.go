package hooks

import (
	"context"
	"sync"
	"time"

	"github.com/apptrail-sh/agent/internal/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BatchConfig holds configuration for event batching
type BatchConfig struct {
	FlushWindow  time.Duration // Time window for batching events
	MaxBatchSize int           // Maximum events per batch
}

// DefaultBatchConfig returns the default batching configuration
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		FlushWindow:  2 * time.Second,
		MaxBatchSize: 100,
	}
}

// ResourceEventPublisher is the interface for publishing resource events (batched)
type ResourceEventPublisher interface {
	PublishBatch(ctx context.Context, events []model.ResourceEventPayload) error
}

// ResourceEventPublisherQueue handles batching and publishing of resource events
type ResourceEventPublisherQueue struct {
	eventChan  <-chan model.ResourceEventPayload
	publishers []ResourceEventPublisher
	config     BatchConfig

	mu      sync.Mutex
	buffer  []model.ResourceEventPayload
	timer   *time.Timer
	stopCh  chan struct{}
	stopped bool
}

// NewResourceEventPublisherQueue creates a new batching resource event publisher queue
func NewResourceEventPublisherQueue(
	eventChan <-chan model.ResourceEventPayload,
	publishers []ResourceEventPublisher,
	config BatchConfig,
) *ResourceEventPublisherQueue {
	return &ResourceEventPublisherQueue{
		eventChan:  eventChan,
		publishers: publishers,
		config:     config,
		buffer:     make([]model.ResourceEventPayload, 0, config.MaxBatchSize),
		stopCh:     make(chan struct{}),
	}
}

// Loop starts the event processing loop
func (q *ResourceEventPublisherQueue) Loop() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	logger.Info("Resource event publisher queue started",
		"publishers", len(q.publishers),
		"flushWindow", q.config.FlushWindow,
		"maxBatchSize", q.config.MaxBatchSize,
	)

	for {
		select {
		case event, ok := <-q.eventChan:
			if !ok {
				// Channel closed, flush remaining events
				q.flush(ctx)
				return
			}
			q.addEvent(ctx, event)

		case <-q.stopCh:
			q.flush(ctx)
			return
		}
	}
}

// Stop stops the publisher queue
func (q *ResourceEventPublisherQueue) Stop() {
	q.mu.Lock()
	if !q.stopped {
		q.stopped = true
		close(q.stopCh)
	}
	q.mu.Unlock()
}

func (q *ResourceEventPublisherQueue) addEvent(ctx context.Context, event model.ResourceEventPayload) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.buffer = append(q.buffer, event)

	// Start timer on first event
	if len(q.buffer) == 1 {
		q.timer = time.AfterFunc(q.config.FlushWindow, func() {
			q.flush(ctx)
		})
	}

	// Flush immediately if batch is full
	if len(q.buffer) >= q.config.MaxBatchSize {
		q.flushLocked(ctx)
	}
}

func (q *ResourceEventPublisherQueue) flush(ctx context.Context) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.flushLocked(ctx)
}

func (q *ResourceEventPublisherQueue) flushLocked(ctx context.Context) {
	if len(q.buffer) == 0 {
		return
	}

	// Stop timer if running
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}

	logger := log.FromContext(ctx)

	// Copy buffer for publishing
	events := make([]model.ResourceEventPayload, len(q.buffer))
	copy(events, q.buffer)

	// Clear buffer
	q.buffer = q.buffer[:0]

	// Publish to all registered publishers
	logger.Info("Flushing resource event batch",
		"eventCount", len(events),
		"publishers", len(q.publishers),
	)

	for _, publisher := range q.publishers {
		if err := publisher.PublishBatch(ctx, events); err != nil {
			logger.Error(err, "Failed to publish resource event batch")
		}
	}
}
