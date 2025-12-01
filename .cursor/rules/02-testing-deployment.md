# Testing & Deployment

## Testing
- Framework: **Ginkgo/Gomega**
- E2E: Use envtest (real K8s API)
- Mock external HTTP calls (Slack, Control Plane, etc.)

```bash
make test    # Unit tests
make e2e     # E2E tests
make lint    # golangci-lint
```

## Key Commands
```bash
make build                           # Build binary
make docker-build IMG=<registry>/apptrail-agent:tag
make deploy IMG=<registry>/apptrail-agent:tag
make generate                        # After kubebuilder marker changes
```

## Runtime Configuration
- `--metrics-bind-address` (default :8080)
- `--health-probe-bind-address` (default :8081)
- `--leader-elect` (enable for HA)
- `--metrics-secure` (serve over HTTPS)
- `--slack-webhook-url`
- `--controlplane-url` (Control Plane HTTP endpoint)
- `--cluster-id` (required for control plane)
- `--environment` (optional, can use environment mapping)
- `--pubsub-topic-id` (GCP Pub/Sub, project auto-detected from credentials)

Env vars: `CLUSTER_ID`, `ENVIRONMENT`, `PUBSUB_TOPIC_ID`

## Before Committing
1. Run `make test && make lint`
2. Update tests if modifying reconciliation
3. Consider metric backward compatibility
