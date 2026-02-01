package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGCPProvider_Detect_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify required header
		if r.Header.Get("Metadata-Flavor") != gcpMetadataFlavor {
			t.Errorf("Expected Metadata-Flavor: Google header")
		}
		w.Header().Set("Metadata-Flavor", gcpMetadataFlavor)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	provider := NewGCPProviderWithURL(client, server.URL+"/computeMetadata/v1")

	ctx := context.Background()
	if !provider.Detect(ctx) {
		t.Error("Expected Detect to return true for GCP metadata server")
	}
}

func TestGCPProvider_Detect_NotGCP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Missing Metadata-Flavor header in response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	provider := NewGCPProviderWithURL(client, server.URL+"/computeMetadata/v1")

	ctx := context.Background()
	if provider.Detect(ctx) {
		t.Error("Expected Detect to return false when Metadata-Flavor header is missing")
	}
}

func TestGCPProvider_Detect_ServerUnavailable(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	provider := NewGCPProviderWithURL(client, "http://192.0.2.1/computeMetadata/v1") // Non-routable IP

	ctx := context.Background()
	if provider.Detect(ctx) {
		t.Error("Expected Detect to return false when server is unavailable")
	}
}

func TestGCPProvider_Resolve_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify required header
		if r.Header.Get("Metadata-Flavor") != gcpMetadataFlavor {
			t.Errorf("Expected Metadata-Flavor: Google header")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Metadata-Flavor", gcpMetadataFlavor)
		switch r.URL.Path {
		case testGCPClusterNamePath:
			_, _ = w.Write([]byte("my-gke-cluster"))
		case testGCPProjectIDPath:
			_, _ = w.Write([]byte("my-gcp-project"))
		case testGCPZonePath:
			_, _ = w.Write([]byte("projects/123456789/zones/us-central1-a"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	provider := NewGCPProviderWithURL(client, server.URL+"/computeMetadata/v1")

	ctx := context.Background()
	info, err := provider.Resolve(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedClusterID := "gcp/my-gcp-project/us-central1/my-gke-cluster"
	if info.ClusterID != expectedClusterID {
		t.Errorf("Expected cluster ID %q, got %q", expectedClusterID, info.ClusterID)
	}

	if info.Provider != ProviderGCP {
		t.Errorf("Expected provider %q, got %q", ProviderGCP, info.Provider)
	}

	if info.Region != "us-central1" {
		t.Errorf("Expected region %q, got %q", "us-central1", info.Region)
	}

	if info.ClusterName != "my-gke-cluster" {
		t.Errorf("Expected cluster name %q, got %q", "my-gke-cluster", info.ClusterName)
	}
}

func TestGCPProvider_Resolve_MissingClusterName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Metadata-Flavor", gcpMetadataFlavor)
		switch r.URL.Path {
		case testGCPClusterNamePath:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	provider := NewGCPProviderWithURL(client, server.URL+"/computeMetadata/v1")

	ctx := context.Background()
	_, err := provider.Resolve(ctx)
	if err == nil {
		t.Error("Expected error when cluster-name is not available")
	}
}

func TestGCPProvider_Name(t *testing.T) {
	provider := NewGCPProvider(&http.Client{})
	if provider.Name() != ProviderGCP {
		t.Errorf("Expected provider name %q, got %q", ProviderGCP, provider.Name())
	}
}

func TestExtractRegionFromZone(t *testing.T) {
	tests := []struct {
		zone     string
		expected string
	}{
		{"us-central1-a", "us-central1"},
		{"us-central1-b", "us-central1"},
		{"europe-west1-b", "europe-west1"},
		{"asia-east1-c", "asia-east1"},
		{"us-east4-a", "us-east4"},
		{"southamerica-east1-a", "southamerica-east1"},
		{"invalid", "invalid"}, // No hyphen, returns as-is
	}

	for _, tt := range tests {
		t.Run(tt.zone, func(t *testing.T) {
			result := extractRegionFromZone(tt.zone)
			if result != tt.expected {
				t.Errorf("extractRegionFromZone(%q) = %q, want %q", tt.zone, result, tt.expected)
			}
		})
	}
}
