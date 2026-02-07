# Agent

Kubernetes controller that watches workloads for version changes and publishes events.

**Language:** Go 1.24.6
**Framework:** Kubebuilder v4 with controller-runtime v0.22.4

## Commands

```bash
# Build
make build                    # Compiles to bin/apptrail
go build -o bin/apptrail cmd/main.go

# Test & Lint
make test                     # Run unit tests with coverage (Ginkgo/Gomega + envtest)
make test-e2e                 # Run E2E tests against Kind k8s cluster
make lint                     # Run golangci-lint
make test && make lint        # Pre-commit check

# Generate Code
make generate                 # Generate DeepCopy methods
make manifests                # Generate CRDs and RBAC

# Deploy
make install                  # Install CRDs to cluster
make deploy IMG=<registry>/agent:tag
make undeploy                 # Remove from cluster

# Docker
make docker-build IMG=<registry>/agent:tag
make docker-push IMG=<registry>/agent:tag
```

## Core Concepts

- Watches workloads (Deployments, StatefulSets, DaemonSets) with `app.kubernetes.io/version` label
- Tracks rollout phases: `rolling_out`, `success`, `failed`, `progressing`
- Uses **WorkloadAdapter pattern** to share reconciliation logic across workload types
- Maintains rollout state via `WorkloadRolloutState` CRD
- Publishes events to Control Plane via HTTP, Slack, or Google Pub/Sub (events include all workload labels: team, git metadata, etc.)
- Custom 15-minute rollout timeout (handles GitOps tools resetting K8s defaults)

## Architecture

**Reconcilers** (`internal/reconciler/`):
- `workload_reconciler.go` - Shared reconciliation logic for all workload types
- `deployment_reconciler.go` - Deployment-specific reconciler
- `statefulset_reconciler.go` - StatefulSet-specific reconciler
- `daemonset_reconciler.go` - DaemonSet-specific reconciler
- `workload.go` - WorkloadAdapter interface and concrete implementations

**Event Publishers** (`internal/hooks/`):
- `notifier.go` - EventPublisher interface
- `notifications.go` - EventPublisherQueue (buffered channel with 100 capacity)
- `controlplane/http.go` - HTTP publisher for Control Plane API
- `slack/slack.go` - Slack webhook publisher
- `pubsub/pubsub.go` - Google Cloud Pub/Sub publisher

**CRDs** (`api/v1alpha1/`):
- `workloadrolloutstate_types.go` - Tracks rollout state per workload

## Key Configuration

```bash
# Core
--controlplane-url=http://controlplane:3000   # Control Plane URL (required for CP publisher)
--cluster-id=staging.stg01                    # Cluster ID (or CLUSTER_ID env var; auto-detected on GCP)
--pubsub-topic=projects/x/topics/y            # GCP Pub/Sub topic (or PUBSUB_TOPIC env var)
--slack-webhook-url=https://hooks.slack.com/...

# Infrastructure tracking
--track-nodes=false                           # Enable node tracking
--track-pods=false                            # Enable pod tracking
--watch-namespaces=""                         # Comma-separated namespace patterns to watch
--exclude-namespaces=kube-system,kube-public,kube-node-lease
--require-labels=""                           # Labels that must be present
--exclude-labels=""                           # Label key=value pairs that cause exclusion

# Heartbeat
--heartbeat-enabled=true                      # Periodic heartbeat to control plane
--heartbeat-interval=5m

# Operator plumbing
--metrics-bind-address=:8080
--health-probe-bind-address=:8081
--leader-elect=false
--metrics-secure=false
--enable-http2=false
```

## Critical Patterns

**Error Handling:**
```go
if err := dr.Get(ctx, req.NamespacedName, resource); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

**Logging:**
```go
log := ctrl.LoggerFrom(ctx)
log.Info("message", "key", value)
```

**Metrics (avoid cardinality explosion):**
```go
appVersionGauge.DeletePartialMatch(labels)  // Delete old series first
appVersionGauge.WithLabelValues(...).Set(1)
```

## Adding a New Event Publisher

1. Create `internal/hooks/<servicename>/` directory
2. Implement `EventPublisher` interface with `Publish(context.Context, model.WorkloadUpdate) error`
3. Register in `cmd/main.go` publishers slice
4. Add configuration flags if needed

## Gotchas

- Version label `app.kubernetes.io/version` is **required** on all tracked workloads
- Leader election ID: `ce02bd06.apptrail.sh`
- Event publisher queue buffer: 100 events
- Rollout timeout: 15 minutes (custom, handles GitOps tool quirks)
- Metrics must delete old series before creating new ones to prevent cardinality explosion
