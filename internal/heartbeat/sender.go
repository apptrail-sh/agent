package heartbeat

import (
	"context"
	"time"

	"github.com/apptrail-sh/agent/internal/hooks"
	"github.com/apptrail-sh/agent/internal/model"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Config holds configuration for the heartbeat sender
type Config struct {
	Interval     time.Duration
	ClusterID    string
	AgentVersion string
	TrackNodes   bool
	TrackPods    bool
}

// DefaultConfig returns the default heartbeat configuration
func DefaultConfig() Config {
	return Config{
		Interval:   5 * time.Minute,
		TrackNodes: true,
		TrackPods:  true,
	}
}

// Sender periodically sends heartbeats to the control plane
type Sender struct {
	config     Config
	client     client.Client
	publishers []hooks.HeartbeatPublisher
	stopCh     chan struct{}
}

// NewSender creates a new heartbeat sender
func NewSender(
	config Config,
	k8sClient client.Client,
	publishers []hooks.HeartbeatPublisher,
) *Sender {
	return &Sender{
		config:     config,
		client:     k8sClient,
		publishers: publishers,
		stopCh:     make(chan struct{}),
	}
}

// Start starts the heartbeat sender loop
func (s *Sender) Start(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("heartbeat-sender")

	logger.Info("Starting heartbeat sender",
		"interval", s.config.Interval,
		"clusterID", s.config.ClusterID,
		"trackNodes", s.config.TrackNodes,
		"trackPods", s.config.TrackPods,
		"publishers", len(s.publishers),
	)

	// Send initial heartbeat immediately
	s.sendHeartbeat(ctx)

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.sendHeartbeat(ctx)
		case <-s.stopCh:
			logger.Info("Heartbeat sender stopped")
			return
		case <-ctx.Done():
			logger.Info("Heartbeat sender context cancelled")
			return
		}
	}
}

// Stop stops the heartbeat sender
func (s *Sender) Stop() {
	close(s.stopCh)
}

func (s *Sender) sendHeartbeat(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("heartbeat-sender")

	// Collect node UIDs if tracking nodes
	var nodeUIDs []string
	if s.config.TrackNodes {
		var err error
		nodeUIDs, err = s.collectNodeUIDs(ctx)
		if err != nil {
			logger.Error(err, "Failed to collect node UIDs")
		}
	}

	// Collect pod UIDs if tracking pods
	var podUIDs []string
	if s.config.TrackPods {
		var err error
		podUIDs, err = s.collectPodUIDs(ctx)
		if err != nil {
			logger.Error(err, "Failed to collect pod UIDs")
		}
	}

	payload := model.NewClusterHeartbeatPayload(
		s.config.ClusterID,
		s.config.AgentVersion,
		nodeUIDs,
		podUIDs,
	)

	logger.Info("Sending heartbeat",
		"eventID", payload.EventID,
		"nodeCount", len(nodeUIDs),
		"podCount", len(podUIDs),
	)

	// Publish to all registered publishers
	for _, publisher := range s.publishers {
		if err := publisher.PublishHeartbeat(ctx, payload); err != nil {
			logger.Error(err, "Failed to publish heartbeat")
		}
	}
}

func (s *Sender) collectNodeUIDs(ctx context.Context) ([]string, error) {
	var nodeList corev1.NodeList
	if err := s.client.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	uids := make([]string, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		uids = append(uids, string(node.UID))
	}

	return uids, nil
}

func (s *Sender) collectPodUIDs(ctx context.Context) ([]string, error) {
	var podList corev1.PodList
	if err := s.client.List(ctx, &podList); err != nil {
		return nil, err
	}

	uids := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		uids = append(uids, string(pod.UID))
	}

	return uids, nil
}
