package model

type WorkloadUpdate struct {
	Name            string
	Namespace       string
	Kind            string
	PreviousVersion string
	CurrentVersion  string
	Labels          map[string]string // Kubernetes labels from the workload
}
