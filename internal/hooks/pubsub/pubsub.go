package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/pubsub/v2"
	"github.com/apptrail-sh/agent/internal/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PubSubPublisher sends workload updates to Google Cloud Pub/Sub
type PubSubPublisher struct {
	client       *pubsub.Client
	publisher    *pubsub.Publisher
	topicPath    string
	clusterID    string
	agentVersion string
}

// ParseTopicPath parses a full Pub/Sub topic path and returns projectID and topicID.
// Expected format: projects/<project>/topics/<topic>
func ParseTopicPath(topicPath string) (projectID, topicID string, err error) {
	parts := strings.Split(topicPath, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[2] != "topics" {
		return "", "", fmt.Errorf("invalid topic path %q: expected format projects/<project>/topics/<topic>", topicPath)
	}
	return parts[1], parts[3], nil
}

// NewPubSubPublisher creates a new Google Cloud Pub/Sub publisher
//
// Authentication is handled via Application Default Credentials (ADC):
//   - Workload Identity (GKE): Auto-detected from metadata server (recommended)
//   - Service Account JSON key: Set GOOGLE_APPLICATION_CREDENTIALS env var
//   - Default credentials: gcloud auth application-default login
//
// Parameters:
//   - topicPath: Full Pub/Sub topic path (projects/<project>/topics/<topic>)
//   - clusterID: Unique identifier for this cluster
//   - agentVersion: Version of the agent
func NewPubSubPublisher(ctx context.Context, topicPath, clusterID, agentVersion string) (*PubSubPublisher, error) {
	projectID, topicID, err := ParseTopicPath(topicPath)
	if err != nil {
		return nil, err
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	// Enable message ordering to guarantee events for the same workload
	// are delivered in the order they were published.
	// The subscription must also have message ordering enabled.
	publisher := client.Publisher(topicID)
	publisher.EnableMessageOrdering = true

	return &PubSubPublisher{
		client:       client,
		publisher:    publisher,
		topicPath:    topicPath,
		clusterID:    clusterID,
		agentVersion: agentVersion,
	}, nil
}

// Publish sends a workload update to Google Cloud Pub/Sub
func (p *PubSubPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	event := model.NewAgentEventPayload(update, p.clusterID, p.agentVersion)

	data, err := json.Marshal(event)
	if err != nil {
		logger.Error(err, "Failed to marshal event",
			"eventID", event.EventID,
			"namespace", event.Workload.Namespace,
			"name", event.Workload.Name,
		)
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Ordering key ensures events for the same cluster are delivered in order.
	// Using cluster ID for all events ensures consistent ordering across all event types.
	orderingKey := p.clusterID

	logger.Info("Publishing event to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", event.EventID,
		"orderingKey", orderingKey,
		"namespace", event.Workload.Namespace,
		"name", event.Workload.Name,
		"currentVersion", event.Revision.Current,
		"previousVersion", event.Revision.Previous,
		"deploymentPhase", event.Phase,
	)

	attributes := map[string]string{
		"cluster_name":  p.clusterID,
		"namespace":     event.Workload.Namespace,
		"workload_name": event.Workload.Name,
		"workload_type": string(event.Workload.Kind),
		"event_type":    string(event.Kind),
	}
	if event.Phase != nil {
		attributes["deployment_phase"] = string(*event.Phase)
	}

	result := p.publisher.Publish(ctx, &pubsub.Message{
		Data:        data,
		Attributes:  attributes,
		OrderingKey: orderingKey,
	})

	msgID, err := result.Get(ctx)
	if err != nil {
		logger.Error(err, "Failed to publish event to Pub/Sub",
			"topic", p.topicPath,
			"eventID", event.EventID,
		)
		return fmt.Errorf("failed to publish event to pubsub: %w", err)
	}

	logger.Info("Event successfully published to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", event.EventID,
		"messageID", msgID,
		"namespace", event.Workload.Namespace,
		"name", event.Workload.Name,
	)

	return nil
}

// PublishBatch sends a batch of resource events to Google Cloud Pub/Sub
// Implements hooks.ResourceEventPublisher interface
func (p *PubSubPublisher) PublishBatch(ctx context.Context, events []model.ResourceEventPayload) error {
	if len(events) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)

	logger.Info("Publishing resource event batch to Google Pub/Sub",
		"topic", p.topicPath,
		"eventCount", len(events),
	)

	var publishResults []*pubsub.PublishResult
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			logger.Error(err, "Failed to marshal resource event",
				"eventID", event.EventID,
				"resourceType", event.ResourceType,
				"name", event.Resource.Name,
			)
			continue
		}

		// Ordering key ensures events for the same cluster are delivered in order.
		// Using cluster ID for all events ensures consistent ordering across all event types.
		orderingKey := p.clusterID

		attributes := map[string]string{
			"cluster_id":    p.clusterID,
			"resource_type": string(event.ResourceType),
			"event_kind":    string(event.EventKind),
			"resource_name": event.Resource.Name,
			"message_type":  "resource_event", // Distinguish from workload events
		}
		if event.Resource.Namespace != "" {
			attributes["namespace"] = event.Resource.Namespace
		}

		result := p.publisher.Publish(ctx, &pubsub.Message{
			Data:        data,
			Attributes:  attributes,
			OrderingKey: orderingKey,
		})
		publishResults = append(publishResults, result)
	}

	// Wait for all publishes to complete
	var errors []error
	for i, result := range publishResults {
		msgID, err := result.Get(ctx)
		if err != nil {
			logger.Error(err, "Failed to publish resource event to Pub/Sub",
				"eventID", events[i].EventID,
			)
			errors = append(errors, err)
		} else {
			logger.V(1).Info("Resource event published",
				"messageID", msgID,
				"eventID", events[i].EventID,
			)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to publish %d/%d events", len(errors), len(events))
	}

	logger.Info("Resource event batch successfully published to Google Pub/Sub",
		"topic", p.topicPath,
		"eventCount", len(events),
	)

	return nil
}

// PublishHeartbeat sends a heartbeat to Google Cloud Pub/Sub
// Implements hooks.HeartbeatPublisher interface
func (p *PubSubPublisher) PublishHeartbeat(ctx context.Context, payload model.ClusterHeartbeatPayload) error {
	logger := log.FromContext(ctx)

	logger.Info("Publishing heartbeat to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", payload.EventID,
		"nodeCount", len(payload.Inventory.NodeUIDs),
		"podCount", len(payload.Inventory.PodUIDs),
	)

	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error(err, "Failed to marshal heartbeat payload",
			"eventID", payload.EventID,
		)
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	// Ordering key ensures events for the same cluster are delivered in order.
	// Using cluster ID for all events ensures consistent ordering across all event types.
	orderingKey := p.clusterID

	attributes := map[string]string{
		"cluster_id":   p.clusterID,
		"message_type": "heartbeat",
	}

	result := p.publisher.Publish(ctx, &pubsub.Message{
		Data:        data,
		Attributes:  attributes,
		OrderingKey: orderingKey,
	})

	msgID, err := result.Get(ctx)
	if err != nil {
		logger.Error(err, "Failed to publish heartbeat to Pub/Sub",
			"topic", p.topicPath,
			"eventID", payload.EventID,
		)
		return fmt.Errorf("failed to publish heartbeat to pubsub: %w", err)
	}

	logger.Info("Heartbeat successfully published to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", payload.EventID,
		"messageID", msgID,
	)

	return nil
}

// Stop stops the publisher and closes the client
func (p *PubSubPublisher) Stop() {
	if p.publisher != nil {
		p.publisher.Stop()
	}
	if p.client != nil {
		_ = p.client.Close()
	}
}
