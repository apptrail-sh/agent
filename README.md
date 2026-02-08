# AppTrail Agent

Kubernetes controller that watches workloads for version changes, tracks rollout progress, and publishes deployment
events to the AppTrail Control Plane. The Agent is the data collection component of the AppTrail multi-cluster
observability platform.

## Description

The AppTrail Agent is a Kubebuilder-based Kubernetes controller that monitors Deployments, StatefulSets, and DaemonSets
with the `app.kubernetes.io/version` label. It tracks rollout phases (`rolling_out`, `success`, `failed`, `progressing`)
with a custom 15-minute timeout to handle GitOps tools, and publishes workload events—including version changes, team
labels, and Git metadata—to configurable destinations: HTTP (Control Plane API), Slack webhooks, or Google Cloud
Pub/Sub.

The Agent maintains rollout state using a custom `WorkloadRolloutState` CRD and uses a shared WorkloadAdapter pattern to
reconcile all workload types with consistent logic.

## Architecture

```
┌────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                │
│                                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │ Deployment   │  │ StatefulSet  │  │ DaemonSet    │  │
│  │ (v1.2.3)     │  │ (v2.0.1)     │  │ (v3.1.0)     │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  │
│         │                 │                 │          │
│         └─────────────────┴─────────────────┘          │
│                           │                            │
│                  ┌────────▼────────┐                   │
│                  │  AppTrail Agent │                   │
│                  │  (Controller)   │                   │
│                  └────────┬────────┘                   │
│                           │                            │
│         ┌─────────────────┼────────────────┐           │
│         │                 │                │           │
│    ┌────▼────┐      ┌─────▼─────┐     ┌────▼────┐      │
│    │   CRDs  │      │    CRDs   │     │   CRDs  │      │
│    └─────────┘      └───────────┘     └─────────┘      │
│                 WorkloadRolloutState CRDs              │
└────────────────────────────────────────┬───────────────┘
                                         │
                       Event Publishers  │
                  ┌────────┬─────────────┴──────────┐
                  │        │                        │
           ┌──────▼─────┐  │                 ┌──────▼──────┐
           │ Control    │  │                 │   Slack     │
           │ Plane HTTP │  │                 │  Webhook    │
           └────────────┘  │                 └─────────────┘
                    ┌──────▼──────┐
                    │ GCP Pub/Sub │
                    └─────────────┘
```

### Core Components

**Reconcilers** (`internal/reconciler/`):

- `workload_reconciler.go` - Shared reconciliation logic for all workload types
- `deployment_reconciler.go`, `statefulset_reconciler.go`, `daemonset_reconciler.go` - Type-specific reconcilers
- `workload.go` - WorkloadAdapter interface with concrete implementations for each workload type

**Event Publishers** (`internal/hooks/`):

- `notifier.go` - EventPublisher interface
- `notifications.go` - EventPublisherQueue with 100-event buffer
- `controlplane/http.go` - HTTP publisher for Control Plane API
- `slack/slack.go` - Slack webhook publisher
- `pubsub/pubsub.go` - Google Cloud Pub/Sub publisher

**CRDs** (`api/v1alpha1/`):

- `workloadrolloutstate_types.go` - Tracks rollout state per workload

## Local Development

### Quick Start

```bash
# Build the binary
make build

# Run unit tests
make test

# Run linters
make lint

# Pre-commit check
make test && make lint

# Generate DeepCopy methods
make generate

# Generate CRDs and RBAC manifests
make manifests
```

### Running Locally

The Agent requires a running Kubernetes cluster and optionally a Control Plane instance. For full-stack local
development, see the [parent repository setup](../.claude/CLAUDE.md).

**Run against your current kubeconfig cluster:**

```bash
make run -- --controlplane-url=http://localhost:3000 \
            --cluster-id=local.dev \
            --environment=development
```

**Build binary directly:**

```bash
go build -o bin/apptrail cmd/main.go
./bin/apptrail --controlplane-url=http://localhost:3000 \
               --cluster-id=local.dev
```

### Code Generation

After modifying API types or adding new types:

```bash
make generate   # Generate DeepCopy methods
make manifests  # Generate CRDs and RBAC
```

## Getting Started

### Prerequisites

- Go 1.24.6+
- Docker 17.03+
- kubectl 1.30+
- Access to a Kubernetes 1.30+ cluster
- `make` (Makefile-based build system)
- `golangci-lint` (for linting)

### Deploy to Cluster

**1. Build and push your image:**

```bash
make docker-build docker-push IMG=<some-registry>/agent:tag
```

**NOTE:** This image must be published to a registry accessible from your cluster.

**2. Install the CRDs:**

```bash
make install
```

**3. Deploy the controller:**

```bash
make deploy IMG=<some-registry>/agent:tag
```

> **NOTE**: If you encounter RBAC errors, you may need cluster-admin privileges.

**4. Configure the deployment:**

Edit the deployment to add required configuration flags:

```bash
kubectl edit deployment apptrail-agent-controller-manager -n apptrail-agent-system
```

Add flags to the container args (see [Configuration](#configuration) below).

### Uninstall

**Delete sample resources:**

```bash
kubectl delete -k config/samples/
```

**Remove CRDs:**

```bash
make uninstall
```

**Remove the controller:**

```bash
make undeploy
```

## Configuration

The Agent is configured via command-line flags or environment variables. Key configuration options:

| Flag                          | Description                                                                | Example                       |
|-------------------------------|----------------------------------------------------------------------------|-------------------------------|
| `--controlplane-url`          | Control Plane API endpoint (required for HTTP publisher)                   | `http://controlplane:3000`    |
| `--cluster-id`                | Cluster identifier (auto-detected on GCP, or set via `CLUSTER_ID` env var) | `staging.stg01`               |
| `--environment`               | Environment name (development, staging, production)                        | `staging`                     |
| `--pubsub-topic`              | GCP Pub/Sub topic for events (or `PUBSUB_TOPIC` env var)                   | `projects/x/topics/y`         |
| `--slack-webhook-url`         | Slack webhook URL for notifications                                        | `https://hooks.slack.com/...` |
| `--watch-namespaces`          | Comma-separated namespace patterns to watch                                | `app-*,web-*`                 |
| `--exclude-namespaces`        | Namespaces to exclude (default: `kube-system,kube-public,kube-node-lease`) | `monitoring,istio-system`     |
| `--require-labels`            | Labels that must be present on workloads                                   | `team`                        |
| `--exclude-labels`            | Label key=value pairs that cause exclusion                                 | `exclude=true`                |
| `--track-nodes`               | Enable node tracking (default: `false`)                                    | `true`                        |
| `--track-pods`                | Enable pod tracking (default: `false`)                                     | `true`                        |
| `--heartbeat-enabled`         | Send periodic heartbeat to Control Plane (default: `true`)                 | `false`                       |
| `--heartbeat-interval`        | Heartbeat interval (default: `5m`)                                         | `10m`                         |
| `--metrics-bind-address`      | Metrics server address (default: `:8080`)                                  | `:9090`                       |
| `--health-probe-bind-address` | Health probe address (default: `:8081`)                                    | `:9091`                       |
| `--leader-elect`              | Enable leader election (default: `false`)                                  | `true`                        |

**Example deployment configuration:**

```bash
./bin/apptrail \
  --controlplane-url=http://controlplane.apptrail.svc.cluster.local:3000 \
  --cluster-id=prod-gke-us-east1 \
  --environment=production \
  --watch-namespaces=production-*,apps-* \
  --exclude-namespaces=kube-system,kube-public \
  --require-labels=team \
  --track-pods=true \
  --leader-elect=true
```

For complete configuration reference, see [.claude/CLAUDE.md](.claude/CLAUDE.md).

## Testing

### Unit Tests

Run unit tests with Ginkgo/Gomega and envtest (in-memory Kubernetes API):

```bash
make test
```

Tests are located in `internal/reconciler/*_test.go` and use the controller-runtime envtest framework.

### E2E Tests

Run end-to-end tests against a Kind cluster:

```bash
make test-e2e
```

This creates a temporary Kind cluster, deploys the Agent, and validates real workload reconciliation.

### Linting

Run golangci-lint:

```bash
make lint
```

### Pre-commit Validation

Before committing code:

```bash
make test && make lint
```

## Key Concepts & Gotchas

**Version Label Requirement:**

- The `app.kubernetes.io/version` label is **required** on all tracked workloads
- Workloads without this label are ignored by the Agent
- Label format is flexible (semantic versions, Git SHAs, timestamps all work)

**Rollout Timeout:**

- Custom 15-minute rollout timeout (not the Kubernetes default)
- Designed to handle GitOps tools that reset default timeout values
- After 15 minutes without progress, rollout is marked as `failed`

**Event Queue:**

- Event publishers use a buffered queue with 100-event capacity
- Events are dropped if the queue is full (logged as warnings)
- Consider tuning publisher concurrency if drops occur frequently

**Leader Election:**

- Leader election ID: `ce02bd06.apptrail.sh`
- Enable with `--leader-elect=true` for multi-replica deployments
- Only the leader performs reconciliation; replicas are hot standby

**Metrics:**

- Prometheus metrics are exposed on `--metrics-bind-address` (default `:8080`)
- Old metric series must be deleted before creating new ones to prevent cardinality explosion
- See `internal/metrics/` for metric definitions

**Cluster ID Auto-detection:**

- On GCP, cluster ID is auto-detected from instance metadata
- Can be overridden with `--cluster-id` flag or `CLUSTER_ID` env var
- Format recommendation: `<env>-<provider>-<region>` (e.g., `prod-gke-us-east1`)

## Full-Stack Integration

The Agent is one component of the AppTrail platform:

- **Agent** (this component) - Collects workload data from Kubernetes clusters
- **Control Plane** - Aggregates data and provides REST API
- **UI** - Web interface for viewing workload versions and history

### Running the Full Stack Locally

For local development of Control Plane or UI, the Agent is **optional**. You can develop the API and UI independently
using mock data or database seeds.

To run all components together:

```bash
# Terminal 1: Control Plane + PostgreSQL
cd ../controlplane
./gradlew bootRun

# Terminal 2: UI
cd ../ui
bun dev

# Terminal 3: Agent (this component)
cd agent
make run -- --controlplane-url=http://localhost:3000 \
            --cluster-id=local.dev \
            --environment=development
```

See [parent repository CLAUDE.md](../.claude/CLAUDE.md) for complete setup instructions.

## Project Distribution

### Build Installer

Generate a single-file YAML manifest for distribution:

```bash
make build-installer IMG=<some-registry>/agent:tag
```

This creates `dist/install.yaml` containing all resources (CRDs, RBAC, Deployment) built with Kustomize.

### Install via Manifest

Users can install the Agent with a single command:

```bash
kubectl apply -f https://raw.githubusercontent.com/<org>/agent/<tag>/dist/install.yaml
```

## Contributing

Contributions are welcome! Please follow these guidelines:

1. **Run pre-commit checks:** `make test && make lint` before submitting PRs
2. **Write tests:** Add unit tests for new reconciliation logic
3. **Update documentation:** Keep README and CLAUDE.md in sync with code changes
4. **Follow conventions:** Use existing code style (gofmt, golangci-lint rules)

### Adding a New Event Publisher

To add support for a new event destination:

1. Create `internal/hooks/<servicename>/` directory
2. Implement `EventPublisher` interface with `Publish(context.Context, model.WorkloadUpdate) error`
3. Register the publisher in `cmd/main.go` publishers slice
4. Add configuration flags if needed

See [.claude/CLAUDE.md](.claude/CLAUDE.md) for detailed instructions.

## Additional Resources

- **Kubebuilder Documentation:** https://book.kubebuilder.io/introduction.html
- **Agent Configuration Reference:** [.claude/CLAUDE.md](.claude/CLAUDE.md)
- **Architecture Details:** [.claude/CLAUDE.md](.claude/CLAUDE.md)
- **Control Plane README:** [../controlplane/README.md](../controlplane/README.md)

**NOTE:** Run `make help` for more information on all available `make` targets.

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
