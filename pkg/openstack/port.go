package openstack

import (
	"context"
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CreatePort creates a port in OpenStack for the service IP
func (c *ClientImpl) CreatePort(
	ctx context.Context,
	networkID, subnetID, portName, ipAddress, description string,
	serviceNamespace, serviceName string,
) (string, error) {
	logger := log.FromContext(ctx)

	// Generate tags for ownership tracking
	tags := map[string]string{
		TagManagedBy:        TagManagedByValue,
		TagServiceNamespace: serviceNamespace,
		TagServiceName:      serviceName,
		TagResourceType:     ResourceTypePort,
	}

	// Get the cluster name from environment if available
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName != "" {
		tags[TagClusterName] = clusterName
	}

	// Create the port without tags first
	portCreateOpts := ports.CreateOpts{
		NetworkID: networkID,
		Name:      portName,
		FixedIPs: []ports.IP{
			{
				SubnetID:  subnetID,
				IPAddress: ipAddress,
			},
		},
		Description: description,
	}

	logger.V(1).Info("Creating OpenStack port",
		"name", portName,
		"networkID", networkID,
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))

	port, err := ports.Create(c.networkClient, portCreateOpts).Extract()
	if err != nil {
		return "", fmt.Errorf("failed to create port: %w", err)
	}

	// Now set the tags on the created port
	tagSlice := convertMapToTags(tags)
	replaceAllOpts := attributestags.ReplaceAllOpts{
		Tags: tagSlice,
	}

	_, err = attributestags.ReplaceAll(c.networkClient, "ports", port.ID, replaceAllOpts).Extract()
	if err != nil {
		// If we fail to set tags, log a warning but don't fail the operation
		logger.Error(err, "Warning: Failed to set tags on port, resource tracking may be affected",
			"portID", port.ID)
	}

	logger.Info("Created OpenStack port", "portID", port.ID, "name", port.Name)
	return port.ID, nil
}

// GetPort checks if a port exists in OpenStack
func (c *ClientImpl) GetPort(ctx context.Context, portID string) (bool, error) {
	_, err := ports.Get(c.networkClient, portID).Extract()
	if err != nil {
		if IsNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get port %s: %w", portID, err)
	}
	return true, nil
}

// DeletePort deletes a port from OpenStack
func (c *ClientImpl) DeletePort(ctx context.Context, portID string) error {
	logger := log.FromContext(ctx)

	// First verify the port exists
	_, err := ports.Get(c.networkClient, portID).Extract()
	if err != nil {
		if IsNotFoundError(err) {
			logger.Info("Port not found, already deleted", "portID", portID)
			return nil
		}
		return fmt.Errorf("failed to get port %s before deletion: %w", portID, err)
	}

	// Get the tags for this port
	tags, err := attributestags.List(c.networkClient, "ports", portID).Extract()
	if err != nil {
		logger.Error(err, "Failed to get tags for port - will attempt deletion anyway",
			"portID", portID)
		// Continue with deletion attempt even if tag check fails
	} else {
		// Check if the port has our ownership tag
		if !isResourceOwnedByOperator(tags) {
			return fmt.Errorf("refusing to delete port %s: not owned by this operator", portID)
		}
	}

	err = ports.Delete(c.networkClient, portID).ExtractErr()
	if err != nil {
		if IsNotFoundError(err) {
			logger.Info("Port not found during deletion, already deleted", "portID", portID)
			return nil
		}
		return fmt.Errorf("failed to delete port %s: %w", portID, err)
	}

	logger.Info("Successfully deleted port", "portID", portID)
	return nil
}

// GetManagedPorts gets all ports managed by this operator for a specific service
func (c *ClientImpl) GetManagedPorts(ctx context.Context, serviceNamespace, serviceName string) ([]ports.Port, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Looking for managed ports", "service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))

	// Unfortunately, we can't filter by tags in the initial list request with older Gophercloud versions
	// So we'll list all ports and filter manually
	allPages, err := ports.List(c.networkClient, ports.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to list ports: %w", err)
	}

	allPorts, err := ports.ExtractPorts(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ports: %w", err)
	}

	var managedPorts []ports.Port
	for _, port := range allPorts {
		// Get tags for this port
		tags, err := attributestags.List(c.networkClient, "ports", port.ID).Extract()
		if err != nil {
			logger.Error(err, "Error getting tags for port", "portID", port.ID)
			continue
		}

		// Check if this port is owned by our operator and is for this service
		if isResourceOwnedByOperator(tags) && isResourceForService(tags, serviceNamespace, serviceName) {
			managedPorts = append(managedPorts, port)
		}
	}

	logger.V(1).Info("Found managed ports", "count", len(managedPorts),
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))
	return managedPorts, nil
}

// Helper function to convert a map of tags to OpenStack's tag format
func convertMapToTags(tagMap map[string]string) []string {
	var tags []string
	for k, v := range tagMap {
		tags = append(tags, fmt.Sprintf("%s=%s", k, v))
	}
	return tags
}

// Helper function to check if a resource is owned by our operator
func isResourceOwnedByOperator(tags []string) bool {
	ownershipTag := fmt.Sprintf("%s=%s", TagManagedBy, TagManagedByValue)
	for _, tag := range tags {
		if tag == ownershipTag {
			return true
		}
	}
	return false
}

// Helper function to check if a resource is for a specific service
func isResourceForService(tags []string, namespace, name string) bool {
	namespaceTag := fmt.Sprintf("%s=%s", TagServiceNamespace, namespace)
	nameTag := fmt.Sprintf("%s=%s", TagServiceName, name)

	hasNamespace := false
	hasName := false

	for _, tag := range tags {
		if tag == namespaceTag {
			hasNamespace = true
		}
		if tag == nameTag {
			hasName = true
		}
	}

	return hasNamespace && hasName
}
