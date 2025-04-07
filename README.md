# OpenStack Load Balancer Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/jacero-io/openstack-lb-operator)
![GitHub last commit](https://img.shields.io/github/last-commit/jacero-io/openstack-lb-operator)
![GitHub issues](https://img.shields.io/github/issues/jacero-io/openstack-lb-operator)
![GitHub pull requests](https://img.shields.io/github/issues-pr/jacero-io/openstack-lb-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/jacero-io/openstack-lb-operator)](https://goreportcard.com/report/github.com/jacero-io/openstack-lb-operator)
![Go Version](https://img.shields.io/badge/Go-v1.22%2B-blue)
![Kubernetes Version](https://img.shields.io/badge/Kubernetes-v1.29.1%2B-blue)
![Docker Version](https://img.shields.io/badge/Docker-v25.0.0%2B-blue)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/openstack-lb-operator)](https://artifacthub.io/packages/helm/openstack-lb-operator/openstack-lb-operator)

## Overview

The OpenStack Load Balancer Operator is a Kubernetes operator that seamlessly integrates Kubernetes LoadBalancer services with OpenStack networking. It automates the creation and management of network ports and floating IPs for Kubernetes services running in OpenStack environments.

## Features

- 🌐 Automatic OpenStack port creation for Kubernetes LoadBalancer services
- 🚀 Dynamic floating IP management
- 🔒 Secure credential management using Kubernetes secrets
- 📦 Multi-architecture support (linux/amd64, linux/arm64)

## Prerequisites

- Kubernetes cluster (v1.24+)
- OpenStack cloud environment
- Application credentials with network access

## Installation

### Using Helm

```bash

helm install openstack-lb-operator oci://ghcr.io/jacero-io/charts/openstack-lb-operator
```

## Configuration

### Create OpenStack Credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openstack-credentials
  namespace: default
type: Opaque
stringData:
  OS_AUTH_URL: https://your-openstack-keystone.com/v3
  OS_REGION_NAME: RegionOne
  OS_APPLICATION_CREDENTIAL_ID: your-app-credential-id
  OS_APPLICATION_CREDENTIAL_SECRET: your-app-credential-secret
```

### Create OpenStackLoadBalancer Resource

```yaml
apiVersion: openstack.jacero.io/v1alpha1
kind: OpenStackLoadBalancer
metadata:
  name: default-lb
  namespace: default
spec:
  className: openstack-lb
  applicationCredentialSecretRef:
    name: openstack-credentials
    namespace: default
  createFloatingIP: true # Set to false if you don't want to create floating IPs
  floatingIPNetworkID: your-external-network-id
```

## Development

### Prerequisites

- Go 1.24+
- Docker
- Kind or another Kubernetes development cluster
- Task (task runner)

### Local Development

1. Clone the repository
2. Set up development environment
3. Run tests

```bash
# Install development dependencies
task util:setup

# Setup cluster
task dev:setup

# Setup Devstack
task devstack:setup

# Run unit tests
task test

# Run end-to-end tests
task test-e2e
```

## Contributing

Contributions are welcome! Please see our [CONTRIBUTING.md](CONTRIBUTING.md) for details on submitting pull requests.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Troubleshooting

- Ensure OpenStack credentials are correct
- Check operator logs for detailed error messages
- Verify network connectivity between Kubernetes and OpenStack

## Roadmap

- [ ] Support Envoy Gateway Service
- [ ] Support labels and namespace targets for more fine grained resource filtering
- [ ] Ability to add annotation to Service directly to trigger the controller
- [ ] Enhanced logging and monitoring

## Maintainers

- Emil Jacero (@jacero-io)

## Acknowledgments

- Kubernetes Community
- OpenStack Project
- Kubebuilder
