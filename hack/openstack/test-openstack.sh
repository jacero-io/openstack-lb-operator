#!/bin/bash
# Source the admin credentials
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller cat /opt/stack/devstack/openrc admin > $(dirname $0)/admin-openrc.sh
source $(dirname $0)/admin-openrc.sh

# Test if the OpenStack services are running
echo "Testing OpenStack services..."
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller openstack service list

# Test network functionality
echo -e "\nTesting network connectivity..."
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller openstack network list

# Check for external network
echo -e "\nChecking for external network..."
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller openstack network list --external

# Test compute functionality
echo -e "\nChecking compute services..."
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller openstack compute service list

# Show endpoint list
echo -e "\nListing API endpoints..."
docker compose -f /var/home/emil/Development/jacero/openstack-lb-operator/hack/openstack/docker-compose.yaml exec controller openstack endpoint list
