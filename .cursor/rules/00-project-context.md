# AppTrail Controller - Project Context

## What It Does
Kubernetes controller that tracks deployment version changes:
- Watches `apps/v1.Deployment` for changes to `app.kubernetes.io/version` label
- Exports Prometheus metric: `apptrail_app_version`
- Sends notifications via pluggable notifiers (Slack implemented)

## Tech Stack
- **Go 1.24.6** with Kubebuilder v4 (controller-runtime v0.22.3)
- **Domain**: apptrail.sh
- **Key Dependencies**: k8s.io/api v0.34.1, prometheus/client_golang v1.23.2

## Project Structure
```
/cmd/main.go                     # Manager setup, notifier registration
/internal/reconciler/deployment.go  # Core reconciliation logic
/internal/hooks/
  - notifier.go                  # Notifier interface
  - notifications.go             # NotifierQueue (channel consumer)
  - slack/slack.go               # Slack implementation
/internal/model/models.go        # WorkloadUpdate model
```

## Key Architecture Patterns

### In-Memory State
Controller maintains `map[string]AppVersion` keyed by `namespace/name` to track version history.

### Channel-Based Notifications
```
Reconciler → chan WorkloadUpdate → NotifierQueue → []Notifier
```
- Buffer size: 100
- Runs in separate goroutine
- Only notifies if `PreviousVersion != ""`
- Errors logged but don't block reconciliation

### Metric Management
**Critical**: Delete old metric series before creating new ones to prevent cardinality explosion:
```go
appVersionGauge.DeletePartialMatch(labels)  // Delete old
appVersionGauge.WithLabelValues(...).Set(1)  // Create new
```

