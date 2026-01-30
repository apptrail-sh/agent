package infrastructure

import (
	"github.com/apptrail-sh/agent/internal/filter"
)

// ResourceFilter is an alias to filter.ResourceFilter for convenience
type ResourceFilter = filter.ResourceFilter

// ResourceFilterConfig is an alias to filter.ResourceFilterConfig for convenience
type ResourceFilterConfig = filter.ResourceFilterConfig

// NewResourceFilter creates a new resource filter
func NewResourceFilter(config ResourceFilterConfig) *ResourceFilter {
	return filter.NewResourceFilter(config)
}
