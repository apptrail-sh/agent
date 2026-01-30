/*
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
*/

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"
	"strings"

	"github.com/apptrail-sh/agent/internal/buildinfo"
	"github.com/apptrail-sh/agent/internal/filter"
	"github.com/apptrail-sh/agent/internal/hooks"
	"github.com/apptrail-sh/agent/internal/hooks/controlplane"
	"github.com/apptrail-sh/agent/internal/hooks/pubsub"
	"github.com/apptrail-sh/agent/internal/hooks/slack"
	"github.com/apptrail-sh/agent/internal/model"

	"github.com/apptrail-sh/agent/internal/reconciler"
	"github.com/apptrail-sh/agent/internal/reconciler/infrastructure"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	apptrailv1alpha1 "github.com/apptrail-sh/agent/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// config holds all command-line configuration
type config struct {
	metricsAddr          string
	enableLeaderElection bool
	probeAddr            string
	secureMetrics        bool
	enableHTTP2          bool
	slackWebhookURL      string
	controlPlaneURL      string
	clusterID            string
	pubsubTopic          string
	trackNodes           bool
	trackPods            bool
	watchNamespaces      string
	excludeNamespaces    string
	requireLabels        string
	excludeLabels        string
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apptrailv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	cfg := parseFlags()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	mgr := setupManager(cfg)
	agentVersion := buildinfo.AgentVersion()

	// Setup channels for event publishing
	publisherChan := make(chan model.WorkloadUpdate, 100)
	resourceEventChan := make(chan model.ResourceEventPayload, 1000)

	// Setup publishers
	publishers, resourcePublishers := setupPublishers(cfg, agentVersion)
	startPublisherQueues(cfg, publisherChan, resourceEventChan, publishers, resourcePublishers)

	// Setup reconcilers
	controllerNamespace := getControllerNamespace()
	setupWorkloadReconcilers(mgr, publisherChan, controllerNamespace)
	setupInfrastructureReconcilers(mgr, cfg, resourceEventChan, agentVersion)

	// +kubebuilder:scaffold:builder

	setupHealthChecks(mgr)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&cfg.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&cfg.secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&cfg.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&cfg.slackWebhookURL, "slack-webhook-url", "", "The URL to send slack notifications to")
	flag.StringVar(&cfg.controlPlaneURL, "controlplane-url", "",
		"The URL of the AppTrail Control Plane (e.g., http://controlplane:3000/ingest/v1/agent/events)")
	flag.StringVar(&cfg.clusterID, "cluster-id", os.Getenv("CLUSTER_ID"),
		"Unique identifier for this cluster (e.g., staging.stg01)")
	flag.StringVar(&cfg.pubsubTopic, "pubsub-topic", os.Getenv("PUBSUB_TOPIC"),
		"Google Cloud Pub/Sub topic path (projects/<project>/topics/<topic>)")

	// Infrastructure tracking flags
	flag.BoolVar(&cfg.trackNodes, "track-nodes", false,
		"Enable tracking of Kubernetes nodes")
	flag.BoolVar(&cfg.trackPods, "track-pods", false,
		"Enable tracking of Kubernetes pods")
	flag.StringVar(&cfg.watchNamespaces, "watch-namespaces", "",
		"Comma-separated list of namespace patterns to watch (e.g., 'production-*,staging-*')")
	flag.StringVar(&cfg.excludeNamespaces, "exclude-namespaces", "kube-system,kube-public,kube-node-lease",
		"Comma-separated list of namespace patterns to exclude")
	flag.StringVar(&cfg.requireLabels, "require-labels", "",
		"Comma-separated list of label keys that must be present (e.g., 'app.kubernetes.io/managed-by')")
	flag.StringVar(&cfg.excludeLabels, "exclude-labels", "",
		"Comma-separated list of label key=value pairs that cause exclusion (e.g., 'internal.apptrail.sh/ignore=true')")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	return cfg
}

func setupManager(cfg config) ctrl.Manager {
	var tlsOpts []func(*tls.Config)

	if !cfg.enableHTTP2 {
		disableHTTP2 := func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	metricsServerOptions := metricsserver.Options{
		BindAddress:   cfg.metricsAddr,
		SecureServing: cfg.secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if cfg.secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.probeAddr,
		LeaderElection:         cfg.enableLeaderElection,
		LeaderElectionID:       "ce02bd06.apptrail.sh",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	return mgr
}

func setupPublishers(cfg config, agentVersion string) ([]hooks.EventPublisher, []hooks.ResourceEventPublisher) {
	var publishers []hooks.EventPublisher
	var resourcePublishers []hooks.ResourceEventPublisher

	if cfg.slackWebhookURL != "" {
		slackPublisher := slack.NewSlackPublisher(cfg.slackWebhookURL)
		publishers = append(publishers, slackPublisher)
		setupLog.Info("Slack publisher enabled", "webhook", cfg.slackWebhookURL)
	}

	if cfg.controlPlaneURL != "" {
		if cfg.clusterID == "" {
			setupLog.Error(nil, "cluster-id is required when controlplane-url is set")
			os.Exit(1)
		}
		cpPublisher := controlplane.NewHTTPPublisher(cfg.controlPlaneURL, cfg.clusterID, agentVersion)
		publishers = append(publishers, cpPublisher)
		resourcePublishers = append(resourcePublishers, cpPublisher)
		setupLog.Info("Control Plane publisher enabled",
			"endpoint", cfg.controlPlaneURL,
			"clusterID", cfg.clusterID)
	}

	if cfg.pubsubTopic != "" {
		if cfg.clusterID == "" {
			setupLog.Error(nil, "cluster-id is required when pubsub is enabled")
			os.Exit(1)
		}
		ctx := context.Background()
		pubsubPublisher, err := pubsub.NewPubSubPublisher(ctx, cfg.pubsubTopic, cfg.clusterID, agentVersion)
		if err != nil {
			setupLog.Error(err, "unable to create Pub/Sub publisher",
				"hint", "Ensure valid credentials via Workload Identity, GOOGLE_APPLICATION_CREDENTIALS, or gcloud auth")
			os.Exit(1)
		}
		publishers = append(publishers, pubsubPublisher)
		resourcePublishers = append(resourcePublishers, pubsubPublisher)
		setupLog.Info("Google Pub/Sub publisher enabled",
			"topic", cfg.pubsubTopic,
			"clusterID", cfg.clusterID)
	}

	if len(publishers) == 0 {
		setupLog.Info("No event publishers configured, events will only be exported as metrics")
	}

	return publishers, resourcePublishers
}

func startPublisherQueues(
	cfg config,
	publisherChan chan model.WorkloadUpdate,
	resourceEventChan chan model.ResourceEventPayload,
	publishers []hooks.EventPublisher,
	resourcePublishers []hooks.ResourceEventPublisher,
) {
	publisherQueue := hooks.NewEventPublisherQueue(publisherChan, publishers)
	go publisherQueue.Loop()

	if len(resourcePublishers) > 0 && (cfg.trackNodes || cfg.trackPods) {
		batchConfig := hooks.DefaultBatchConfig()
		resourcePublisherQueue := hooks.NewResourceEventPublisherQueue(resourceEventChan, resourcePublishers, batchConfig)
		go resourcePublisherQueue.Loop()
		setupLog.Info("Resource event publisher queue started",
			"trackNodes", cfg.trackNodes,
			"trackPods", cfg.trackPods,
		)
	}
}

func getControllerNamespace() string {
	controllerNamespace := os.Getenv("POD_NAMESPACE")
	if controllerNamespace == "" {
		controllerNamespace = "apptrail-system"
		setupLog.Info("POD_NAMESPACE not set, using default", "namespace", controllerNamespace)
	}
	return controllerNamespace
}

func setupWorkloadReconcilers(mgr ctrl.Manager, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string) {
	deploymentReconciler := reconciler.NewDeploymentReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("apptrail-agent"),
		publisherChan,
		controllerNamespace)

	if err := deploymentReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppTrailDeployment")
		os.Exit(1)
	}

	statefulSetReconciler := reconciler.NewStatefulSetReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("apptrail-agent"),
		publisherChan,
		controllerNamespace)

	if err := statefulSetReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppTrailStatefulSet")
		os.Exit(1)
	}

	daemonSetReconciler := reconciler.NewDaemonSetReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("apptrail-agent"),
		publisherChan,
		controllerNamespace)

	if err := daemonSetReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppTrailDaemonSet")
		os.Exit(1)
	}
}

func setupInfrastructureReconcilers(
	mgr ctrl.Manager,
	cfg config,
	resourceEventChan chan<- model.ResourceEventPayload,
	agentVersion string,
) {
	if !cfg.trackNodes && !cfg.trackPods {
		return
	}

	filterConfig := filter.ResourceFilterConfig{
		TrackNodes:        cfg.trackNodes,
		TrackPods:         cfg.trackPods,
		TrackServices:     false,
		WatchNamespaces:   splitAndTrim(cfg.watchNamespaces),
		ExcludeNamespaces: splitAndTrim(cfg.excludeNamespaces),
		RequireLabels:     splitAndTrim(cfg.requireLabels),
		ExcludeLabels:     splitAndTrim(cfg.excludeLabels),
	}

	resourceFilter := filter.NewResourceFilter(filterConfig)

	if cfg.trackNodes {
		nodeReconciler := infrastructure.NewNodeReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor("apptrail-agent"),
			resourceEventChan,
			cfg.clusterID,
			agentVersion,
		)
		if err := nodeReconciler.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "AppTrailNode")
			os.Exit(1)
		}
		setupLog.Info("Node reconciler enabled")
	}

	if cfg.trackPods {
		podReconciler := infrastructure.NewPodReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor("apptrail-agent"),
			resourceEventChan,
			cfg.clusterID,
			agentVersion,
			resourceFilter,
		)
		if err := podReconciler.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "AppTrailPod")
			os.Exit(1)
		}
		setupLog.Info("Pod reconciler enabled",
			"excludeNamespaces", filterConfig.ExcludeNamespaces,
		)
	}
}

func setupHealthChecks(mgr ctrl.Manager) {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
