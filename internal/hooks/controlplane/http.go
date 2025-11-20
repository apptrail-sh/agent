package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/apptrail-sh/controller/internal/model"
	"github.com/google/uuid"
	"resty.dev/v3"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// HTTPPublisher sends workload updates to the AppTrail Control Plane via HTTP
type HTTPPublisher struct {
	client      *resty.Client
	endpoint    string
	clusterID   string
	environment string
}

// NewHTTPPublisher creates a new HTTP publisher for the control plane
func NewHTTPPublisher(endpoint, clusterID, environment string) *HTTPPublisher {
	client := resty.New().
		SetTimeout(10 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	return &HTTPPublisher{
		client:      client,
		endpoint:    endpoint,
		clusterID:   clusterID,
		environment: environment,
	}
}

// Event represents the event structure expected by the control plane
type Event struct {
	ID              string            `json:"id"`
	Labels          map[string]string `json:"labels"`
	ApplicationName string            `json:"applicationName"`
	Namespace       string            `json:"namespace"`
	EventType       string            `json:"eventType"`
	WorkloadType    string            `json:"workloadType"`
	PreviousVersion string            `json:"previousVersion"`
	CurrentVersion  string            `json:"currentVersion"`
}

// Publish sends a workload update to the control plane
func (p *HTTPPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	// Build event - control plane will determine actual event type using semver
	// Merge Kubernetes labels with cluster metadata
	labels := make(map[string]string)

	// Copy all Kubernetes labels from the workload
	if update.Labels != nil {
		for k, v := range update.Labels {
			labels[k] = v
		}
	}

	// Add cluster metadata (overrides if conflicts)
	labels["cluster_name"] = p.clusterID

	// Add environment only if provided (optional for namespace-mapped environments)
	if p.environment != "" {
		labels["environment"] = p.environment
	}

	event := Event{
		ID:              uuid.New().String(),
		ApplicationName: update.Name,
		Namespace:       update.Namespace,
		EventType:       "deployment", // Generic type, control plane determines upgrade/rollback
		WorkloadType:    update.Kind,
		PreviousVersion: update.PreviousVersion,
		CurrentVersion:  update.CurrentVersion,
		Labels:          labels,
	}

	logger.Info("Publishing event to control plane",
		"endpoint", p.endpoint,
		"eventID", event.ID,
		"namespace", update.Namespace,
		"name", update.Name,
		"currentVersion", update.CurrentVersion,
		"previousVersion", update.PreviousVersion,
	)

	// Send request with Resty
	var errorResponse map[string]interface{}
	resp, err := p.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(event).
		SetError(&errorResponse).
		Post(p.endpoint)

	if err != nil {
		logger.Error(err, "Failed to send event to control plane",
			"endpoint", p.endpoint,
			"eventID", event.ID,
		)
		return fmt.Errorf("failed to send event to control plane: %w", err)
	}

	// Check response
	if !resp.IsSuccess() {
		logger.Error(nil, "Control plane returned error",
			"statusCode", resp.StatusCode(),
			"status", resp.Status(),
			"error", errorResponse,
			"body", resp.String(),
			"endpoint", p.endpoint,
			"eventID", event.ID,
		)
		return fmt.Errorf("control plane returned error status %d: %s", resp.StatusCode(), resp.String())
	}

	logger.Info("Event successfully published to control plane",
		"endpoint", p.endpoint,
		"eventID", event.ID,
		"statusCode", resp.StatusCode(),
		"namespace", update.Namespace,
		"name", update.Name,
	)

	return nil
}
