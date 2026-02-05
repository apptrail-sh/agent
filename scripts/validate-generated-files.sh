#!/bin/bash
# Validates that generated files are up to date before release.
# This prevents the +dirty version suffix caused by regenerating files during build.

set -e

echo "Validating generated files are up to date..."

make generate manifests fmt
go mod tidy

if ! git diff --exit-code; then
    echo ""
    echo "ERROR: Generated files are out of date."
    echo "Run 'make generate manifests fmt && go mod tidy', then commit the changes."
    exit 1
fi

echo "Generated files are up to date."
