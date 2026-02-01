package cluster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

const (
	gcpMetadataHost   = "metadata.google.internal"
	gcpMetadataBase   = "http://metadata.google.internal/computeMetadata/v1"
	gcpMetadataFlavor = "Google"
)

// GCPProvider implements cluster ID resolution for GCP/GKE
type GCPProvider struct {
	client      *http.Client
	metadataURL string
}

// NewGCPProvider creates a new GCP provider
func NewGCPProvider(client *http.Client) *GCPProvider {
	return &GCPProvider{
		client:      client,
		metadataURL: gcpMetadataBase,
	}
}

// NewGCPProviderWithURL creates a GCP provider with a custom metadata URL (for testing)
func NewGCPProviderWithURL(client *http.Client, metadataURL string) *GCPProvider {
	return &GCPProvider{
		client:      client,
		metadataURL: metadataURL,
	}
}

// Name returns the provider name
func (p *GCPProvider) Name() CloudProvider {
	return ProviderGCP
}

// Detect checks if running on GCP by querying the metadata server
func (p *GCPProvider) Detect(ctx context.Context) bool {
	// Quick check: try to reach the metadata server
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.metadataURL+"/", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Metadata-Flavor", gcpMetadataFlavor)

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// GCP metadata server returns 200 and Metadata-Flavor: Google header
	return resp.StatusCode == http.StatusOK &&
		resp.Header.Get("Metadata-Flavor") == gcpMetadataFlavor
}

// Resolve retrieves cluster information from GCP metadata
func (p *GCPProvider) Resolve(ctx context.Context) (*ClusterInfo, error) {
	// Get cluster name from instance attributes
	clusterName, err := p.getMetadata(ctx, "/instance/attributes/cluster-name")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster-name: %w", err)
	}

	// Get project ID
	projectID, err := p.getMetadata(ctx, "/project/project-id")
	if err != nil {
		return nil, fmt.Errorf("failed to get project-id: %w", err)
	}

	// Get zone and extract region
	zone, err := p.getMetadata(ctx, "/instance/zone")
	if err != nil {
		return nil, fmt.Errorf("failed to get zone: %w", err)
	}

	// Zone format: projects/<project-number>/zones/<zone>
	// Extract just the zone name
	zoneName := path.Base(zone)
	region := extractRegionFromZone(zoneName)

	// Build cluster ID: gcp/<project-id>/<region>/<cluster-name>
	clusterID := fmt.Sprintf("gcp/%s/%s/%s", projectID, region, clusterName)

	return &ClusterInfo{
		ClusterID:   clusterID,
		Provider:    ProviderGCP,
		Region:      region,
		ClusterName: clusterName,
	}, nil
}

// getMetadata fetches a value from the GCP metadata server
func (p *GCPProvider) getMetadata(ctx context.Context, path string) (string, error) {
	url := p.metadataURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", gcpMetadataFlavor)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

// extractRegionFromZone extracts region from zone (e.g., us-central1-a -> us-central1)
func extractRegionFromZone(zone string) string {
	// Zone format: region-zone (e.g., us-central1-a)
	// Region is everything except the last part after the last hyphen
	lastDash := strings.LastIndex(zone, "-")
	if lastDash == -1 {
		return zone
	}
	return zone[:lastDash]
}
