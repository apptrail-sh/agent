package controlplane

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/apptrail-sh/agent/internal/model"
	"resty.dev/v3"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Compress batches larger than 10KB
	compressionThreshold = 10 * 1024
)

// HTTPPublisher sends workload updates to the AppTrail Control Plane via HTTP
type HTTPPublisher struct {
	client        *resty.Client
	endpoint      string
	batchEndpoint string
	clusterID     string
	agentVersion  string
}

// NewHTTPPublisher creates a new HTTP publisher for the control plane
func NewHTTPPublisher(endpoint, clusterID, agentVersion string) *HTTPPublisher {
	client := resty.New().
		SetTimeout(10 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	// Derive batch endpoint from base endpoint
	// e.g., /ingest/v1/agent/events -> /ingest/v1/agent/events/batch
	batchEndpoint := strings.TrimSuffix(endpoint, "/") + "/batch"

	return &HTTPPublisher{
		client:        client,
		endpoint:      endpoint,
		batchEndpoint: batchEndpoint,
		clusterID:     clusterID,
		agentVersion:  agentVersion,
	}
}

// Publish sends a workload update to the control plane
func (p *HTTPPublisher) Publish(ctx context.Context, update model.WorkloadUpdate) error {
	logger := log.FromContext(ctx)

	event := model.NewAgentEventPayload(update, p.clusterID, p.agentVersion)

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

// PublishBatch sends a batch of resource events to the control plane
// Implements hooks.ResourceEventPublisher interface
func (p *HTTPPublisher) PublishBatch(ctx context.Context, events []model.ResourceEventPayload) error {
	if len(events) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)

	logger.Info("Publishing resource event batch to control plane",
		"endpoint", p.batchEndpoint,
		"eventCount", len(events),
	)

	// Serialize events
	jsonData, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	// Compress if above threshold
	var body []byte
	var contentEncoding string
	if len(jsonData) > compressionThreshold {
		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		if _, err := gzWriter.Write(jsonData); err != nil {
			_ = gzWriter.Close()
			return fmt.Errorf("failed to compress events: %w", err)
		}
		if err := gzWriter.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer: %w", err)
		}
		body = buf.Bytes()
		contentEncoding = "gzip"
		logger.V(1).Info("Compressed batch",
			"originalSize", len(jsonData),
			"compressedSize", len(body),
		)
	} else {
		body = jsonData
	}

	// Build request
	req := p.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)

	if contentEncoding != "" {
		req.SetHeader("Content-Encoding", contentEncoding)
	}

	var errorResponse map[string]interface{}
	req.SetError(&errorResponse)

	resp, err := req.Post(p.batchEndpoint)
	if err != nil {
		logger.Error(err, "Failed to send batch to control plane",
			"endpoint", p.batchEndpoint,
			"eventCount", len(events),
		)
		return fmt.Errorf("failed to send batch to control plane: %w", err)
	}

	if !resp.IsSuccess() {
		logger.Error(nil, "Control plane returned error for batch",
			"statusCode", resp.StatusCode(),
			"status", resp.Status(),
			"error", errorResponse,
			"body", resp.String(),
			"endpoint", p.batchEndpoint,
		)
		return fmt.Errorf("control plane returned error status %d: %s", resp.StatusCode(), resp.String())
	}

	logger.Info("Batch successfully published to control plane",
		"endpoint", p.batchEndpoint,
		"eventCount", len(events),
		"statusCode", resp.StatusCode(),
	)

	return nil
}
