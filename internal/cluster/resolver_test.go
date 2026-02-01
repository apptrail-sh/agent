package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testGCPMetadataPath    = "/computeMetadata/v1"
	testGCPClusterNamePath = "/computeMetadata/v1/instance/attributes/cluster-name"
	testGCPProjectIDPath   = "/computeMetadata/v1/project/project-id"
	testGCPZonePath        = "/computeMetadata/v1/instance/zone"
)

func TestResolver_Resolve_GCP(t *testing.T) {
	// Create a GCP metadata server that would succeed
	gcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Metadata-Flavor", gcpMetadataFlavor)
		switch r.URL.Path {
		case testGCPMetadataPath + "/":
			w.WriteHeader(http.StatusOK)
		case testGCPClusterNamePath:
			_, _ = w.Write([]byte("test-cluster"))
		case testGCPProjectIDPath:
			_, _ = w.Write([]byte("test-project"))
		case testGCPZonePath:
			_, _ = w.Write([]byte("projects/123/zones/us-central1-a"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gcpServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	gcpProvider := NewGCPProviderWithURL(client, gcpServer.URL+testGCPMetadataPath)

	resolver := &Resolver{
		config:    DefaultConfig(),
		providers: []Provider{gcpProvider},
	}

	ctx := context.Background()
	info, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedID := "gcp/test-project/us-central1/test-cluster"
	if info.ClusterID != expectedID {
		t.Errorf("Expected cluster ID %q, got %q", expectedID, info.ClusterID)
	}

	if info.Provider != ProviderGCP {
		t.Errorf("Expected provider %q, got %q", ProviderGCP, info.Provider)
	}
}

func TestResolver_Resolve_NoProvider(t *testing.T) {
	resolver := &Resolver{
		config:    DefaultConfig(),
		providers: []Provider{},
	}

	ctx := context.Background()
	_, err := resolver.Resolve(ctx)
	if err != ErrNoProviderDetected {
		t.Errorf("Expected ErrNoProviderDetected, got: %v", err)
	}
}

func TestResolver_Resolve_ProviderNotDetected(t *testing.T) {
	// Create a server that returns 404 (not GCP)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	gcpProvider := NewGCPProviderWithURL(client, server.URL+testGCPMetadataPath)

	resolver := &Resolver{
		config:    DefaultConfig(),
		providers: []Provider{gcpProvider},
	}

	ctx := context.Background()
	_, err := resolver.Resolve(ctx)
	if err != ErrNoProviderDetected {
		t.Errorf("Expected ErrNoProviderDetected, got: %v", err)
	}
}

func TestResolver_DetectProvider(t *testing.T) {
	// Create GCP metadata server
	gcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Metadata-Flavor", gcpMetadataFlavor)
		w.WriteHeader(http.StatusOK)
	}))
	defer gcpServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	gcpProvider := NewGCPProviderWithURL(client, gcpServer.URL+testGCPMetadataPath)

	resolver := &Resolver{
		config:    DefaultConfig(),
		providers: []Provider{gcpProvider},
	}

	ctx := context.Background()
	provider := resolver.DetectProvider(ctx)
	if provider != ProviderGCP {
		t.Errorf("Expected provider %q, got %q", ProviderGCP, provider)
	}
}

func TestResolver_DetectProvider_NoProvider(t *testing.T) {
	resolver := &Resolver{
		config:    DefaultConfig(),
		providers: []Provider{},
	}

	ctx := context.Background()
	provider := resolver.DetectProvider(ctx)
	if provider != ProviderUnknown {
		t.Errorf("Expected provider %q, got %q", ProviderUnknown, provider)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Timeout != 3*time.Second {
		t.Errorf("Expected timeout 3s, got %v", cfg.Timeout)
	}
	if !cfg.EnableGCP {
		t.Error("Expected EnableGCP to be true")
	}
}

func TestNewResolver(t *testing.T) {
	cfg := DefaultConfig()
	resolver := NewResolver(cfg)

	if len(resolver.providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(resolver.providers))
	}

	if resolver.providers[0].Name() != ProviderGCP {
		t.Errorf("Expected GCP provider, got %q", resolver.providers[0].Name())
	}
}

func TestNewResolver_DisabledGCP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableGCP = false
	resolver := NewResolver(cfg)

	if len(resolver.providers) != 0 {
		t.Errorf("Expected 0 providers, got %d", len(resolver.providers))
	}
}
