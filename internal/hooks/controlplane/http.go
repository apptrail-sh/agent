package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/apptrail-sh/agent/internal/model"
	"resty.dev/v3"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// HTTPPublisher sends workload updates to the AppTrail Control Plane via HTTP
type HTTPPublisher struct {
	client       *resty.Client
	endpoint     string
	clusterID    string
	environment  string
	agentVersion string
}

// NewHTTPPublisher creates a new HTTP publisher for the control plane
func NewHTTPPublisher(endpoint, clusterID, environment, agentVersion string) *HTTPPublisher {
	client := resty.New().
		SetTimeout(10 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	return &HTTPPublisher{
		client:       client,
		endpoint:     endpoint,
		clusterID:    clusterID,
		environment:  environment,
		agentVersion: agentVersion,
	}
}

// Publish sends a workload update to the control plane
func (p *HTTPPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	event := model.NewAgentEventPayload(update, p.clusterID, p.environment, p.agentVersion)

	logger.Info("Publishing event to control plane",
		"endpoint", p.endpoint,
		"eventID", event.EventID,
		"namespace", event.Workload.Namespace,
		"name", event.Workload.Name,
		"currentVersion", event.Revision.Current,
		"previousVersion", event.Revision.Previous,
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
			"eventID", event.EventID,
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
			"eventID", event.EventID,
		)
		return fmt.Errorf("control plane returned error status %d: %s", resp.StatusCode(), resp.String())
	}

	logger.Info("Event successfully published to control plane",
		"endpoint", p.endpoint,
		"eventID", event.EventID,
		"statusCode", resp.StatusCode(),
		"namespace", event.Workload.Namespace,
		"name", event.Workload.Name,
	)

	return nil
}
