package openstack

import (
	"context"
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CreateFloatingIP creates a floating IP in OpenStack and associates it with the port
func (c *ClientImpl) CreateFloatingIP(
	ctx context.Context,
	networkID, portID, description string,
	serviceNamespace, serviceName string,
) (string, string, error) {
	logger := log.FromContext(ctx)

	// Generate tags for ownership tracking
	tags := map[string]string{
		TagManagedBy:        TagManagedByValue,
		TagServiceNamespace: serviceNamespace,
		TagServiceName:      serviceName,
		TagResourceType:     ResourceTypeFloatingIP,
	}

	// Get the cluster name from environment if available
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName != "" {
		tags[TagClusterName] = clusterName
	}

	// Create floating IP without tags first
	floatingIPCreateOpts := floatingips.CreateOpts{
		FloatingNetworkID: networkID,
		PortID:            portID,
		Description:       description,
	}

	logger.Info("Creating OpenStack floating IP",
		"networkID", networkID, "portID", portID,
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))

	floatingIP, err := floatingips.Create(c.networkClient, floatingIPCreateOpts).Extract()
	if err != nil {
		return "", "", fmt.Errorf("failed to create floating IP: %w", err)
	}

	// Now set the tags on the created floating IP
	tagSlice := convertMapToTags(tags)
	replaceAllOpts := attributestags.ReplaceAllOpts{
		Tags: tagSlice,
	}

	_, err = attributestags.ReplaceAll(c.networkClient, "floatingips", floatingIP.ID, replaceAllOpts).Extract()
	if err != nil {
		// If we fail to set tags, log a warning but don't fail the operation
		logger.Error(err, "Warning: Failed to set tags on floating IP, resource tracking may be affected",
			"floatingIPID", floatingIP.ID)
	}

	logger.Info("Created OpenStack floating IP",
		"floatingIPID", floatingIP.ID, "address", floatingIP.FloatingIP)

	return floatingIP.ID, floatingIP.FloatingIP, nil
}

// GetFloatingIP checks if a floating IP exists in OpenStack
func (c *ClientImpl) GetFloatingIP(ctx context.Context, floatingIPID string) (bool, error) {
	_, err := floatingips.Get(c.networkClient, floatingIPID).Extract()
	if err != nil {
		if IsNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get floating IP %s: %w", floatingIPID, err)
	}
	return true, nil
}

// GetFloatingIPByPortID retrieves a floating IP associated with the given port ID
func (c *ClientImpl) GetFloatingIPByPortID(ctx context.Context, portID string) (string, string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Looking for existing floating IP for port", "portID", portID)

	// List all floating IPs
	allPages, err := floatingips.List(c.networkClient, floatingips.ListOpts{
		PortID: portID,
	}).AllPages()
	if err != nil {
		return "", "", fmt.Errorf("failed to list floating IPs: %w", err)
	}

	allFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return "", "", fmt.Errorf("failed to extract floating IPs: %w", err)
	}

	// If we found a floating IP for this port, return its ID and address
	if len(allFIPs) > 0 {
		fip := allFIPs[0]
		logger.Info("Found existing floating IP", "floatingIPID", fip.ID, "address", fip.FloatingIP)
		return fip.ID, fip.FloatingIP, nil
	}

	// No floating IP found for this port
	return "", "", nil
}

// DeleteFloatingIP deletes a floating IP from OpenStack
func (c *ClientImpl) DeleteFloatingIP(ctx context.Context, floatingIPID string) error {
	logger := log.FromContext(ctx)

	// First verify the floating IP exists
	_, err := floatingips.Get(c.networkClient, floatingIPID).Extract()
	if err != nil {
		if IsNotFoundError(err) {
			logger.Info("Floating IP not found, already deleted", "floatingIPID", floatingIPID)
			return nil
		}
		return fmt.Errorf("failed to get floating IP %s before deletion: %w", floatingIPID, err)
	}

	// Get the tags for this floating IP
	tags, err := attributestags.List(c.networkClient, "floatingips", floatingIPID).Extract()
	if err != nil {
		logger.Error(err, "Failed to get tags for floating IP - will attempt deletion anyway",
			"floatingIPID", floatingIPID)
		// Continue with deletion attempt even if tag check fails
	} else {
		// Check if the floating IP has our ownership tag
		if !isResourceOwnedByOperator(tags) {
			return fmt.Errorf("refusing to delete floating IP %s: not owned by this operator", floatingIPID)
		}
	}

	err = floatingips.Delete(c.networkClient, floatingIPID).ExtractErr()
	if err != nil {
		if IsNotFoundError(err) {
			logger.Info("Floating IP not found during deletion, already deleted", "floatingIPID", floatingIPID)
			return nil
		}
		return fmt.Errorf("failed to delete floating IP %s: %w", floatingIPID, err)
	}

	logger.Info("Successfully deleted floating IP", "floatingIPID", floatingIPID)
	return nil
}

// GetManagedFloatingIPs gets all floating IPs managed by this operator for a specific service
func (c *ClientImpl) GetManagedFloatingIPs(ctx context.Context, serviceNamespace, serviceName string) ([]floatingips.FloatingIP, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Looking for managed floating IPs", "service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))

	// List all floating IPs (can't filter by tags in the initial request)
	allPages, err := floatingips.List(c.networkClient, floatingips.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to list floating IPs: %w", err)
	}

	allFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract floating IPs: %w", err)
	}

	var managedFIPs []floatingips.FloatingIP
	for _, fip := range allFIPs {
		// Get tags for this floating IP
		tags, err := attributestags.List(c.networkClient, "floatingips", fip.ID).Extract()
		if err != nil {
			logger.Error(err, "Error getting tags for floating IP", "floatingIPID", fip.ID)
			continue
		}

		// Check if this floating IP is owned by our operator and is for this service
		if isResourceOwnedByOperator(tags) && isResourceForService(tags, serviceNamespace, serviceName) {
			managedFIPs = append(managedFIPs, fip)
		}
	}

	logger.V(1).Info("Found managed floating IPs", "count", len(managedFIPs),
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))
	return managedFIPs, nil
}
