package model

import (
	"time"

	"github.com/google/uuid"
)

// ClusterHeartbeatPayload is the payload sent to the control plane
// to indicate the agent is alive and report current resource inventory
type ClusterHeartbeatPayload struct {
	EventID     string            `json:"eventId"`
	OccurredAt  time.Time         `json:"occurredAt"`
	Source      SourceMetadata    `json:"source"`
	MessageType string            `json:"messageType"`
	Inventory   ResourceInventory `json:"inventory"`
}

// ResourceInventory contains UIDs of all active nodes and pods in the cluster
type ResourceInventory struct {
	NodeUIDs []string `json:"nodeUids,omitempty"`
	PodUIDs  []string `json:"podUids,omitempty"`
}

// NewClusterHeartbeatPayload creates a new heartbeat payload
func NewClusterHeartbeatPayload(
	clusterID, agentVersion string,
	nodeUIDs, podUIDs []string,
) ClusterHeartbeatPayload {
	return ClusterHeartbeatPayload{
		EventID:    uuid.New().String(),
		OccurredAt: time.Now().UTC(),
		Source: SourceMetadata{
			ClusterID:    clusterID,
			AgentVersion: agentVersion,
		},
		MessageType: "HEARTBEAT",
		Inventory: ResourceInventory{
			NodeUIDs: nodeUIDs,
			PodUIDs:  podUIDs,
		},
	}
}
