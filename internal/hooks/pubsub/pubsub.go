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
	environment  string
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
//   - environment: Optional environment name
func NewPubSubPublisher(ctx context.Context, topicPath, clusterID, environment, agentVersion string) (*PubSubPublisher, error) {
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
		environment:  environment,
		agentVersion: agentVersion,
	}, nil
}

// Publish sends a workload update to Google Cloud Pub/Sub
func (p *PubSubPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	event := model.NewAgentEventPayload(update, p.clusterID, p.environment, p.agentVersion)

	data, err := json.Marshal(event)
	if err != nil {
		logger.Error(err, "Failed to marshal event",
			"eventID", event.EventID,
			"namespace", event.Workload.Namespace,
			"name", event.Workload.Name,
		)
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Ordering key ensures events for the same workload are delivered in order.
	// Format: cluster/namespace/workload_name
	orderingKey := fmt.Sprintf("%s/%s/%s", p.clusterID, event.Workload.Namespace, event.Workload.Name)

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
	if p.environment != "" {
		attributes["environment"] = p.environment
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

// Stop stops the publisher and closes the client
func (p *PubSubPublisher) Stop() {
	if p.publisher != nil {
		p.publisher.Stop()
	}
	if p.client != nil {
		_ = p.client.Close()
	}
}
