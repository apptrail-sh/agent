package filter

import (
	"path/filepath"
	"strings"
)

// ResourceFilterConfig holds the configuration for resource filtering
type ResourceFilterConfig struct {
	// Namespace filtering
	WatchNamespaces   []string // Glob patterns for namespaces to watch (e.g., "production-*")
	ExcludeNamespaces []string // Glob patterns for namespaces to exclude (e.g., "kube-system")

	// Label filtering
	RequireLabels []string // Label keys that must be present (e.g., "app.kubernetes.io/managed-by")
	ExcludeLabels []string // Label key=value pairs that cause exclusion (e.g., "internal.apptrail.sh/ignore=true")

	// Resource type toggles
	TrackNodes    bool
	TrackPods     bool
	TrackServices bool
}

// ResourceFilter implements namespace and label-based resource filtering
type ResourceFilter struct {
	config ResourceFilterConfig
}

// NewResourceFilter creates a new resource filter
func NewResourceFilter(config ResourceFilterConfig) *ResourceFilter {
	return &ResourceFilter{config: config}
}

// ShouldWatchNamespace returns true if the namespace should be watched
func (f *ResourceFilter) ShouldWatchNamespace(namespace string) bool {
	// Check exclusions first
	for _, pattern := range f.config.ExcludeNamespaces {
		if matchGlob(pattern, namespace) {
			return false
		}
	}

	// If no watch patterns specified, watch all (that aren't excluded)
	if len(f.config.WatchNamespaces) == 0 {
		return true
	}

	// Check if namespace matches any watch pattern
	for _, pattern := range f.config.WatchNamespaces {
		if matchGlob(pattern, namespace) {
			return true
		}
	}

	return false
}

// ShouldWatchResource returns true if the resource should be watched based on labels
func (f *ResourceFilter) ShouldWatchResource(labels map[string]string) bool {
	// Check required labels
	for _, requiredKey := range f.config.RequireLabels {
		if _, exists := labels[requiredKey]; !exists {
			return false
		}
	}

	// Check exclusion labels
	for _, exclusion := range f.config.ExcludeLabels {
		key, value := parseKeyValue(exclusion)
		if labelValue, exists := labels[key]; exists {
			if value == "" || labelValue == value {
				return false
			}
		}
	}

	return true
}

// ShouldTrackNodes returns true if node tracking is enabled
func (f *ResourceFilter) ShouldTrackNodes() bool {
	return f.config.TrackNodes
}

// ShouldTrackPods returns true if pod tracking is enabled
func (f *ResourceFilter) ShouldTrackPods() bool {
	return f.config.TrackPods
}

// ShouldTrackServices returns true if service tracking is enabled
func (f *ResourceFilter) ShouldTrackServices() bool {
	return f.config.TrackServices
}

// matchGlob performs a simple glob match (supports * wildcard)
func matchGlob(pattern, s string) bool {
	// Use filepath.Match for simple glob matching
	matched, err := filepath.Match(pattern, s)
	if err != nil {
		return false
	}
	return matched
}

// parseKeyValue parses a "key=value" or "key" string
func parseKeyValue(s string) (key, value string) {
	parts := strings.SplitN(s, "=", 2)
	key = parts[0]
	if len(parts) > 1 {
		value = parts[1]
	}
	return
}

// DefaultExcludedNamespaces returns the default list of namespaces to exclude
func DefaultExcludedNamespaces() []string {
	return []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
	}
}
