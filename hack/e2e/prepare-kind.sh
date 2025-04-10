#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Build the operator image for e2e tests
echo "Building operator image..."
DOCKER_BUILDKIT=1 docker build . -t example.com/openstack-lb-operator:test

# Load the image into kind
echo "Loading image into kind..."
kind load docker-image example.com/openstack-lb-operator:test

# Update kustomization to use local image
echo "Updating kustomization..."
sed -i.bak 's|ghcr.io/jacero-io/openstack-lb-operator:v0.0.0-latest|example.com/openstack-lb-operator:test|g' config/manager/manager.yaml

echo "Image prepared for e2e tests successfully!"
