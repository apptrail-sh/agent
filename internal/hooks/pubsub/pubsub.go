package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
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
	topicID     string
	clusterID   string
	environment string
}

// NewPubSubPublisher creates a new Google Cloud Pub/Sub publisher
//
// Authentication and project ID are handled via Application Default Credentials (ADC):
//   - Workload Identity (GKE): Auto-detected from metadata server (recommended)
//   - Service Account JSON key: Set GOOGLE_APPLICATION_CREDENTIALS env var
//   - Default credentials: gcloud auth application-default login
//
// Parameters:
//   - topicID: Pub/Sub topic ID to publish events to
//   - clusterID: Unique identifier for this cluster
//   - environment: Optional environment name
func NewPubSubPublisher(ctx context.Context, topicID, clusterID, environment string) (*PubSubPublisher, error) {
	// Use DetectProjectID to auto-detect from ADC (Workload Identity, env vars, or credentials file)
	client, err := pubsub.NewClient(ctx, pubsub.DetectProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	publisher := client.Publisher(topicID)

	return &PubSubPublisher{
		client:      client,
		publisher:   publisher,
		topicID:     topicID,
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
	DeploymentPhase   string `json:"deploymentPhase,omitempty"`
	ReplicasTotal     int32  `json:"replicasTotal,omitempty"`
	ReplicasReady     int32  `json:"replicasReady,omitempty"`
	ReplicasUpdated   int32  `json:"replicasUpdated,omitempty"`
	ReplicasAvailable int32  `json:"replicasAvailable,omitempty"`
	StatusMessage     string `json:"statusMessage,omitempty"`
	StatusReason      string `json:"statusReason,omitempty"`
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
		DeploymentPhase:   update.DeploymentPhase,
		ReplicasTotal:     update.ReplicasTotal,
		ReplicasReady:     update.ReplicasReady,
		ReplicasUpdated:   update.ReplicasUpdated,
		ReplicasAvailable: update.ReplicasAvailable,
		StatusMessage:     update.StatusMessage,
		StatusReason:      update.StatusReason,
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

	logger.Info("Publishing event to Google Pub/Sub",
		"topic", p.topicID,
		"eventID", event.ID,
		"namespace", update.Namespace,
		"name", update.Name,
		"currentVersion", update.CurrentVersion,
		"previousVersion", update.PreviousVersion,
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
		Data:       data,
		Attributes: attributes,
	})

	msgID, err := result.Get(ctx)
	if err != nil {
		logger.Error(err, "Failed to publish event to Pub/Sub",
			"topic", p.topicID,
			"eventID", event.ID,
		)
		return fmt.Errorf("failed to publish event to pubsub: %w", err)
	}

	logger.Info("Event successfully published to Google Pub/Sub",
		"topic", p.topicID,
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
		p.client.Close()
	}
}
