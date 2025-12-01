package model

type WorkloadUpdate struct {
	Name            string
	Namespace       string
	Kind            string
	PreviousVersion string
	CurrentVersion  string
	Labels          map[string]string // Kubernetes labels from the workload

	// Deployment status
	DeploymentPhase   string // rolling_out, success, failed
	ReplicasTotal     int32
	ReplicasReady     int32
	ReplicasUpdated   int32
	ReplicasAvailable int32
	StatusMessage     string
	StatusReason      string
}
