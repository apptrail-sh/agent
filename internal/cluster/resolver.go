package cluster

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// CloudProvider represents the detected cloud provider
type CloudProvider string

const (
	ProviderUnknown CloudProvider = "unknown"
	ProviderGCP     CloudProvider = "gcp"
)

// ClusterInfo contains resolved cluster identification information
type ClusterInfo struct {
	ClusterID   string
	ClusterName string
	Provider    CloudProvider
	Region      string
	ProjectID   string // Cloud provider project/account ID (e.g., GCP project ID)
}

// ErrNoProviderDetected is returned when no cloud provider can be detected
var ErrNoProviderDetected = errors.New("no cloud provider detected")

// Provider defines the interface for cloud-specific cluster ID resolution
type Provider interface {
	// Name returns the provider identifier
	Name() CloudProvider
	// Detect checks if running on this cloud provider
	Detect(ctx context.Context) bool
	// Resolve retrieves cluster information from the cloud metadata service
	Resolve(ctx context.Context) (*ClusterInfo, error)
}

// Config holds configuration for the resolver
type Config struct {
	// Timeout for metadata requests
	Timeout time.Duration
	// EnableGCP enables GCP/GKE detection
	EnableGCP bool
}

// DefaultConfig returns the default resolver configuration
func DefaultConfig() Config {
	return Config{
		Timeout:   3 * time.Second,
		EnableGCP: true,
	}
}

// Resolver orchestrates cloud provider detection and cluster ID resolution
type Resolver struct {
	config    Config
	providers []Provider
}

// NewResolver creates a new resolver with GCP provider
func NewResolver(cfg Config) *Resolver {
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}

	var providers []Provider

	if cfg.EnableGCP {
		providers = append(providers, NewGCPProvider(httpClient))
	}

	return &Resolver{
		config:    cfg,
		providers: providers,
	}
}

// Resolve detects the cloud provider and resolves the cluster ID
func (r *Resolver) Resolve(ctx context.Context) (*ClusterInfo, error) {
	for _, provider := range r.providers {
		if provider.Detect(ctx) {
			return provider.Resolve(ctx)
		}
	}
	return nil, ErrNoProviderDetected
}

// DetectProvider returns the detected cloud provider without resolving cluster ID
func (r *Resolver) DetectProvider(ctx context.Context) CloudProvider {
	for _, provider := range r.providers {
		if provider.Detect(ctx) {
			return provider.Name()
		}
	}
	return ProviderUnknown
}
