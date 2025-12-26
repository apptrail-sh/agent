package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/apptrail-sh/agent/internal/model"
	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PubSubPublisher sends workload updates to Google Cloud Pub/Sub
type PubSubPublisher struct {
	client      *pubsub.Client
	publisher   *pubsub.Publisher
	topicPath   string
	clusterID   string
	environment string
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
func NewPubSubPublisher(ctx context.Context, topicPath, clusterID, environment string) (*PubSubPublisher, error) {
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
		client:      client,
		publisher:   publisher,
		topicPath:   topicPath,
		clusterID:   clusterID,
		environment: environment,
	}, nil
}

// Event represents the event structure for Pub/Sub messages
type Event struct {
	ID              string            `json:"id"`
	Timestamp       string            `json:"timestamp"`
	Labels          map[string]string `json:"labels"`
	ApplicationName string            `json:"applicationName"`
	Namespace       string            `json:"namespace"`
	EventType       string            `json:"eventType"`
	WorkloadType    string            `json:"workloadType"`
	PreviousVersion string            `json:"previousVersion"`
	CurrentVersion  string            `json:"currentVersion"`

	// Deployment phase tracking
	DeploymentPhase string `json:"deploymentPhase,omitempty"`
	StatusMessage   string `json:"statusMessage,omitempty"`
	StatusReason    string `json:"statusReason,omitempty"`
}

// Publish sends a workload update to Google Cloud Pub/Sub
func (p *PubSubPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	// Build labels - merge Kubernetes labels with cluster metadata
	labels := make(map[string]string)

	// Copy all Kubernetes labels from the workload
	if update.Labels != nil {
		for k, v := range update.Labels {
			labels[k] = v
		}
	}

	labels["cluster_name"] = p.clusterID

	if p.environment != "" {
		labels["environment"] = p.environment
	}

	event := Event{
		ID:              uuid.New().String(),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		ApplicationName: update.Name,
		Namespace:       update.Namespace,
		EventType:       "deployment",
		WorkloadType:    update.Kind,
		PreviousVersion: update.PreviousVersion,
		CurrentVersion:  update.CurrentVersion,
		Labels:          labels,

		// Deployment phase tracking
		DeploymentPhase: update.DeploymentPhase,
		StatusMessage:   update.StatusMessage,
		StatusReason:    update.StatusReason,
	}

	data, err := json.Marshal(event)
	if err != nil {
		logger.Error(err, "Failed to marshal event",
			"eventID", event.ID,
			"namespace", update.Namespace,
			"name", update.Name,
		)
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Ordering key ensures events for the same workload are delivered in order.
	// Format: cluster/namespace/workload_name
	orderingKey := fmt.Sprintf("%s/%s/%s", p.clusterID, update.Namespace, update.Name)

	logger.Info("Publishing event to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", event.ID,
		"orderingKey", orderingKey,
		"namespace", update.Namespace,
		"name", update.Name,
		"currentVersion", update.CurrentVersion,
		"previousVersion", update.PreviousVersion,
		"deploymentPhase", update.DeploymentPhase,
	)

	attributes := map[string]string{
		"cluster_name":  p.clusterID,
		"namespace":     update.Namespace,
		"workload_name": update.Name,
		"workload_type": update.Kind,
		"event_type":    "deployment",
	}
	if p.environment != "" {
		attributes["environment"] = p.environment
	}
	if update.DeploymentPhase != "" {
		attributes["deployment_phase"] = update.DeploymentPhase
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
			"eventID", event.ID,
		)
		return fmt.Errorf("failed to publish event to pubsub: %w", err)
	}

	logger.Info("Event successfully published to Google Pub/Sub",
		"topic", p.topicPath,
		"eventID", event.ID,
		"messageID", msgID,
		"namespace", update.Namespace,
		"name", update.Name,
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
