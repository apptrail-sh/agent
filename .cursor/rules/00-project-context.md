# AppTrail Agent - Project Context

## What It Does
Kubernetes controller that tracks workload version changes:
- Watches `apps/v1.Deployment`, `apps/v1.StatefulSet`, `apps/v1.DaemonSet` for changes to `app.kubernetes.io/version` label
- Tracks rollout phases (rolling_out, success, failed) with 15-minute timeout
- Exports Prometheus metric: `apptrail_app_version`
- Sends events via pluggable publishers (Control Plane HTTP, Slack, GCP Pub/Sub)

## Tech Stack
- **Go 1.24.6** with Kubebuilder v4 (controller-runtime v0.22.3)
- **Domain**: apptrail.sh
- **Key Dependencies**: k8s.io/api v0.34.1, prometheus/client_golang v1.23.2

## Project Structure
```
/cmd/main.go                            # Manager setup, publisher registration
/internal/reconciler/
  - workload.go                         # WorkloadAdapter interface
  - workload_reconciler.go              # Shared reconciliation logic
  - deployment_reconciler.go            # Deployment-specific reconciler
  - statefulset_reconciler.go           # StatefulSet-specific reconciler
  - daemonset_reconciler.go             # DaemonSet-specific reconciler
/internal/hooks/
  - notifier.go                         # EventPublisher interface
  - notifications.go                    # EventPublisherQueue (channel consumer)
  - controlplane/http.go                # Control Plane HTTP publisher
  - slack/slack.go                      # Slack publisher
  - pubsub/pubsub.go                    # GCP Pub/Sub publisher
/internal/model/models.go               # WorkloadUpdate model
/api/v1alpha1/                          # WorkloadRolloutState CRD
```

## Key Architecture Patterns

### WorkloadAdapter Pattern
Abstracts common operations across Deployments, StatefulSets, and DaemonSets:
```go
type WorkloadAdapter interface {
    GetName() string
    GetNamespace() string
    GetKind() string
    GetLabels() map[string]string
    GetVersion() string
    IsRollingOut() bool
    HasFailed() bool
}
```

### Channel-Based Event Publishing
```
Reconciler → chan WorkloadUpdate → EventPublisherQueue → []EventPublisher
```
- Buffer size: 100
- Runs in separate goroutine
- Errors logged but don't block reconciliation

### Metric Management
**Critical**: Delete old metric series before creating new ones to prevent cardinality explosion:
```go
appVersionGauge.DeletePartialMatch(labels)  // Delete old
appVersionGauge.WithLabelValues(...).Set(1)  // Create new
```

### Rollout Phase Tracking
- Phases: `rolling_out`, `success`, `failed`
- 15-minute timeout forces `failed` if rollout exceeds limit (handles Flux/ArgoCD resetting progressDeadlineSeconds)
- Phase changes trigger events even when version unchanged
