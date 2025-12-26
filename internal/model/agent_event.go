package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type AgentEventKind string
type AgentEventOutcome string
type WorkloadKind string
type DeploymentPhase string

const (
	AgentEventKindDeployment AgentEventKind = "DEPLOYMENT"

	AgentEventOutcomeSucceeded AgentEventOutcome = "SUCCEEDED"
	AgentEventOutcomeFailed    AgentEventOutcome = "FAILED"

	WorkloadKindDeployment  WorkloadKind = "DEPLOYMENT"
	WorkloadKindStatefulSet WorkloadKind = "STATEFULSET"
	WorkloadKindDaemonSet   WorkloadKind = "DAEMONSET"
	WorkloadKindJob         WorkloadKind = "JOB"
	WorkloadKindCronJob     WorkloadKind = "CRONJOB"

	DeploymentPhasePending     DeploymentPhase = "PENDING"
	DeploymentPhaseProgressing DeploymentPhase = "PROGRESSING"
	DeploymentPhaseCompleted   DeploymentPhase = "COMPLETED"
	DeploymentPhaseFailed      DeploymentPhase = "FAILED"
)

type SourceMetadata struct {
	ClusterID    string `json:"clusterId"`
	AgentVersion string `json:"agentVersion"`
}

type WorkloadRef struct {
	Kind      WorkloadKind `json:"kind"`
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
}

type Revision struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}

type ErrorDetail struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type AgentEventPayload struct {
	EventID     string             `json:"eventId"`
	OccurredAt  time.Time          `json:"occurredAt"`
	Environment string             `json:"environment"`
	Source      SourceMetadata     `json:"source"`
	Workload    WorkloadRef        `json:"workload"`
	Labels      map[string]string  `json:"labels"`
	Kind        AgentEventKind     `json:"kind"`
	Outcome     *AgentEventOutcome `json:"outcome,omitempty"`
	Revision    *Revision          `json:"revision,omitempty"`
	Phase       *DeploymentPhase   `json:"phase,omitempty"`
	Error       *ErrorDetail       `json:"error,omitempty"`
}

func NewAgentEventPayload(update WorkloadUpdate, clusterID, environment, agentVersion string) AgentEventPayload {
	labels := make(map[string]string)
	if update.Labels != nil {
		for key, value := range update.Labels {
			labels[key] = value
		}
	}

	labels["cluster_name"] = clusterID

	phase := mapDeploymentPhase(update.DeploymentPhase)
	outcome := mapDeploymentOutcome(phase)
	var errorDetail *ErrorDetail
	if update.StatusMessage != "" || update.StatusReason != "" {
		message := update.StatusMessage
		if message == "" {
			message = update.StatusReason
		}
		errorDetail = &ErrorDetail{
			Code:    update.StatusReason,
			Message: message,
		}
	}

	revision := &Revision{
		Current:  update.CurrentVersion,
		Previous: update.PreviousVersion,
	}

	return AgentEventPayload{
		EventID:     uuid.New().String(),
		OccurredAt:  time.Now().UTC(),
		Environment: environment,
		Source: SourceMetadata{
			ClusterID:    clusterID,
			AgentVersion: agentVersion,
		},
		Workload: WorkloadRef{
			Kind:      mapWorkloadKind(update.Kind),
			Name:      update.Name,
			Namespace: update.Namespace,
		},
		Labels:   labels,
		Kind:     AgentEventKindDeployment,
		Outcome:  outcome,
		Revision: revision,
		Phase:    phase,
		Error:    errorDetail,
	}
}

func mapWorkloadKind(kind string) WorkloadKind {
	switch strings.ToLower(kind) {
	case "deployment":
		return WorkloadKindDeployment
	case "statefulset":
		return WorkloadKindStatefulSet
	case "daemonset":
		return WorkloadKindDaemonSet
	case "job":
		return WorkloadKindJob
	case "cronjob":
		return WorkloadKindCronJob
	default:
		return WorkloadKindDeployment
	}
}

func mapDeploymentPhase(phase string) *DeploymentPhase {
	switch phase {
	case "rolling_out":
		value := DeploymentPhaseProgressing
		return &value
	case "success":
		value := DeploymentPhaseCompleted
		return &value
	case "failed":
		value := DeploymentPhaseFailed
		return &value
	default:
		return nil
	}
}

func mapDeploymentOutcome(phase *DeploymentPhase) *AgentEventOutcome {
	if phase == nil {
		return nil
	}
	switch *phase {
	case DeploymentPhaseCompleted:
		value := AgentEventOutcomeSucceeded
		return &value
	case DeploymentPhaseFailed:
		value := AgentEventOutcomeFailed
		return &value
	default:
		return nil
	}
}
