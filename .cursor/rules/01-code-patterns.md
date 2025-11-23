# Code Patterns & Conventions

## Controller Reconciliation

### Standard Error Handling
```go
if err := dr.Get(ctx, req.NamespacedName, resource); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

### Structured Logging
```go
log := ctrl.LoggerFrom(ctx)
log.Info("message", "key", value)
log.Error(err, "error message", "context", value)
```

### Version Change Detection
```go
versionLabel := resource.Labels["app.kubernetes.io/version"]
if versionLabel == "" {
    return ctrl.Result{}, nil  // Skip if no version label
}

appkey := req.Namespace + "/" + req.Name
stored := dr.deploymentVersions[appkey]

if stored.CurrentVersion != versionLabel {
    // Version changed - update state, metrics, notify
}
```

## Metrics

### Registration (Once, in Constructor)
```go
func NewDeploymentReconciler(...) *DeploymentReconciler {
    metrics.Registry.MustRegister(appVersionGauge)  // Only here!
    return &DeploymentReconciler{...}
}
```

### Updating Metrics
```go
// 1. Delete old series (prevent cardinality explosion)
deleted := appVersionGauge.DeletePartialMatch(labelsToDelete)

// 2. Create new series
appVersionGauge.WithLabelValues(
    namespace, name, prevVersion, currVersion, timestamp,
).Set(1)
```

**Never** register metrics in reconcile loop - will panic on duplicate registration.

## Adding a Notifier

### 1. Implement Interface
```go
// internal/hooks/discord/discord.go
type DiscordNotifier struct {
    WebhookURL string
}

func (d *DiscordNotifier) Notify(ctx context.Context, workload model.WorkloadUpdate) error {
    log := ctrl.LoggerFrom(ctx)
    // Send notification, handle errors
    return nil
}
```

### 2. Register in main.go
```go
var discordWebhookURL string
flag.StringVar(&discordWebhookURL, "discord-webhook-url", "", "Discord webhook")

// After flag.Parse()
if discordWebhookURL != "" {
    notifiers = append(notifiers, discord.NewDiscordNotifier(discordWebhookURL))
}
```

## Common Gotchas

1. **Version label required**: Controller skips Deployments without `app.kubernetes.io/version`
2. **Metric cardinality**: Always delete old series before creating new ones
3. **Notification failures**: Should log but not block reconciliation
4. **Leader election**: ID is `ce02bd06.apptrail.sh`
5. **HTTP/2 disabled**: By default for CVE mitigation

