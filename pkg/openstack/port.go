package openstack

import (
	"context"
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/pagination"
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
		TagResourceType:     string(ResourceTypePort),
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
	return deleteResource(
		ctx,
		c.networkClient,
		ResourceTypePort,
		portID,
		func(client *gophercloud.ServiceClient, id string) (bool, error) {
			_, err := ports.Get(client, id).Extract()
			if err != nil {
				if IsNotFoundError(err) {
					return false, nil
				}
				return false, fmt.Errorf("failed to get port %s: %w", id, err)
			}
			return true, nil
		},
		func(id string) error {
			return ports.Delete(c.networkClient, id).ExtractErr()
		},
	)
}

// GetManagedPorts gets all ports managed by this operator for a specific service
func (c *ClientImpl) GetManagedPorts(
	ctx context.Context,
	serviceNamespace, serviceName string,
) ([]ports.Port, error) {
	return getManagedResources(
		ctx,
		c.networkClient,
		string(ResourceTypePort), // Convert ResourceType to string
		"ports",
		serviceNamespace,
		serviceName,
		func() (pagination.Page, error) {
			return ports.List(c.networkClient, ports.ListOpts{}).AllPages()
		},
		ports.ExtractPorts,
		func(port ports.Port) string {
			return port.ID
		},
	)
}
