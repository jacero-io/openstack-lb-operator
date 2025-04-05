package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/pagination"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceType represents the type of OpenStack resource
type ResourceType string

// deleteResource is a common helper for deleting OpenStack resources with proper ownership verification
func deleteResource(
	ctx context.Context,
	client *gophercloud.ServiceClient,
	resourceType ResourceType,
	resourceID string,
	checkExists func(client *gophercloud.ServiceClient, id string) (bool, error),
	deleteFunc func(id string) error,
) error {
	logger := log.FromContext(ctx)

	// First verify the resource exists
	exists, err := checkExists(client, resourceID)
	if err != nil {
		return fmt.Errorf("failed to check if %s %s exists: %w", resourceType, resourceID, err)
	}

	if !exists {
		logger.Info(fmt.Sprintf("%s not found, already deleted", resourceType), "resourceID", resourceID)
		return nil
	}

	// Get the tags for this resource
	tags, err := attributestags.List(client, string(resourceType), resourceID).Extract()
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to get tags for %s - will attempt deletion anyway", resourceType),
			"resourceID", resourceID)
		// Continue with deletion attempt even if tag check fails
	} else {
		// Check if the resource has our ownership tag
		if !isResourceOwnedByOperator(tags) {
			return fmt.Errorf("refusing to delete %s %s: not owned by this operator", resourceType, resourceID)
		}
	}

	err = deleteFunc(resourceID)
	if err != nil {
		if IsNotFoundError(err) {
			logger.Info(fmt.Sprintf("%s not found during deletion, already deleted", resourceType), "resourceID", resourceID)
			return nil
		}
		return fmt.Errorf("failed to delete %s %s: %w", resourceType, resourceID, err)
	}

	logger.Info(fmt.Sprintf("Successfully deleted %s", resourceType), "resourceID", resourceID)
	return nil
}

// isResourceOwnedByOperator is a helper function to determine resource ownership
func isResourceOwnedByOperator(tags []string) bool {
	ownershipTag := fmt.Sprintf("%s=%s", TagManagedBy, TagManagedByValue)
	for _, tag := range tags {
		if tag == ownershipTag {
			return true
		}
	}
	return false
}

// isResourceForService checks if a resource is for a specific service
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

// convertMapToTags converts a map of key-value pairs to OpenStack tag format
func convertMapToTags(tagMap map[string]string) []string {
	// Pre-allocate the slice based on the map size
	tags := make([]string, 0, len(tagMap))

	for k, v := range tagMap {
		tags = append(tags, fmt.Sprintf("%s=%s", k, v))
	}
	return tags
}

// getManagedResources is a generic helper function to get managed resources of a specific type
func getManagedResources[T any](
	ctx context.Context,
	client *gophercloud.ServiceClient,
	resourceType string,
	resourceTypeName string,
	serviceNamespace, serviceName string,
	listFunc func() (pagination.Page, error),
	extractFunc func(pagination.Page) ([]T, error),
	getIDFunc func(T) string,
) ([]T, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info(fmt.Sprintf("Looking for managed %s", resourceTypeName),
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))

	// List all resources
	allPages, err := listFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", resourceTypeName, err)
	}

	allResources, err := extractFunc(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract %s: %w", resourceTypeName, err)
	}

	var managedResources []T
	for _, resource := range allResources {
		resourceID := getIDFunc(resource)
		// Get tags for this resource
		tags, err := attributestags.List(client, resourceType, resourceID).Extract()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Error getting tags for %s", resourceTypeName),
				fmt.Sprintf("%sID", resourceTypeName), resourceID)
			continue
		}

		// Check if this resource is owned by our operator and is for this service
		if isResourceOwnedByOperator(tags) && isResourceForService(tags, serviceNamespace, serviceName) {
			managedResources = append(managedResources, resource)
		}
	}

	logger.V(1).Info(fmt.Sprintf("Found managed %s", resourceTypeName), "count", len(managedResources),
		"service", fmt.Sprintf("%s/%s", serviceNamespace, serviceName))
	return managedResources, nil
}
