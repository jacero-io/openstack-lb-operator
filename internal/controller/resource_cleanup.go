package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/jacero-io/openstack-lb-operator/pkg/openstack"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceCleanupHandler handles the cleanup of OpenStack resources
type ResourceCleanupHandler struct {
	Client          client.Client
	OpenStackClient openstack.Client
}

// NewResourceCleanupHandler creates a new instance of ResourceCleanupHandler
func NewResourceCleanupHandler(k8sClient client.Client, osClient openstack.Client) *ResourceCleanupHandler {
	return &ResourceCleanupHandler{
		Client:          k8sClient,
		OpenStackClient: osClient,
	}
}

// CleanupResourcesForService cleans up all OpenStack resources associated with a service
func (h *ResourceCleanupHandler) CleanupResourcesForService(ctx context.Context, svc *corev1.Service) (bool, error) {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)
	logger.Info("Cleaning up OpenStack resources for service")

	needsRequeue := false

	// Get all resources in batch
	allFloatingIPs, err := h.OpenStackClient.GetAllFloatingIPs(ctx)
	if err != nil {
		logger.Error(err, "Failed to get floating IPs from OpenStack")
		return true, err
	}

	allPorts, err := h.OpenStackClient.GetAllPorts(ctx)
	if err != nil {
		logger.Error(err, "Failed to get ports from OpenStack")
		return true, err
	}

	// Get resource IDs from annotations
	portID, floatingIPID := extractResourceIDsFromAnnotations(svc)

	// Log what we found in annotations
	if portID != "" || floatingIPID != "" {
		logger.V(1).Info("Found resource IDs in service annotations",
			"portID", portID,
			"floatingIPID", floatingIPID)
	}

	// Find and delete floating IPs (they must be deleted before ports)
	floatingIPsToDelete := h.identifyFloatingIPsToDelete(allFloatingIPs, svc, portID, floatingIPID)
	logger.V(1).Info("Identified floating IPs for deletion", "count", len(floatingIPsToDelete))

	// Track which annotations to update
	annotationsToUpdate := make(map[string]string)

	// Delete floating IPs
	for _, fip := range floatingIPsToDelete {
		logger.V(1).Info("Deleting floating IP", "floatingIPID", fip.ID, "address", fip.FloatingIP)
		if err := h.OpenStackClient.DeleteFloatingIP(ctx, fip.ID); err != nil {
			logger.Error(err, "Failed to delete floating IP", "floatingIPID", fip.ID)
			needsRequeue = true
		} else {
			logger.Info("Successfully deleted floating IP", "floatingIPID", fip.ID)
			// If this was the floating IP from our annotation, mark it for removal
			if fip.ID == floatingIPID {
				annotationsToUpdate[AnnotationFloatingIPID] = ""
			}
		}
	}

	// Find and delete ports
	portsToDelete := h.identifyPortsToDelete(allPorts, svc, portID)
	logger.V(1).Info("Identified ports for deletion", "count", len(portsToDelete))

	// Delete ports
	for _, port := range portsToDelete {
		logger.V(1).Info("Deleting port", "portID", port.ID, "name", port.Name)
		if err := h.OpenStackClient.DeletePort(ctx, port.ID); err != nil {
			logger.Error(err, "Failed to delete port", "portID", port.ID)
			needsRequeue = true
		} else {
			logger.Info("Successfully deleted port", "portID", port.ID)
			// If this was the port from our annotation, mark it for removal
			if port.ID == portID {
				annotationsToUpdate[AnnotationPortID] = ""
			}
		}
	}

	// Update service annotations if needed
	if len(annotationsToUpdate) > 0 {
		logger.Info("Updating service annotations to remove resource references",
			"annotationsToRemove", len(annotationsToUpdate))
		if err := updateMultipleServiceAnnotations(ctx, h.Client, svc, annotationsToUpdate); err != nil {
			logger.Error(err, "Failed to update service annotations")
			needsRequeue = true
		}
	}

	// Check if we need to requeue for more cleanup
	if needsRequeue {
		logger.Info("Resource cleanup not complete, will requeue")
		return true, nil
	}

	// Check if service still has our annotations with non-empty values
	if hasAnnotation(svc, AnnotationPortID) || hasAnnotation(svc, AnnotationFloatingIPID) {
		logger.Info("Service still has resource annotations, will need another cleanup pass")
		return true, nil
	}

	logger.Info("All resources cleaned up successfully")
	return false, nil
}

// CleanupResourcesForServiceWithForce cleans up resources more aggressively
func (h *ResourceCleanupHandler) CleanupResourcesForServiceWithForce(ctx context.Context, svc *corev1.Service) (bool, error) {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)
	logger.Info("Aggressively cleaning up OpenStack resources for service")

	needsRequeue := false

	// Get all resources in batch
	_, err := h.OpenStackClient.GetAllFloatingIPs(ctx)
	if err != nil {
		logger.Error(err, "Failed to get floating IPs from OpenStack")
		// Continue anyway to try to remove annotations
	}

	_, err = h.OpenStackClient.GetAllPorts(ctx)
	if err != nil {
		logger.Error(err, "Failed to get ports from OpenStack")
		// Continue anyway to try to remove annotations
	}

	// Get resource IDs from annotations
	portID, floatingIPID := extractResourceIDsFromAnnotations(svc)

	// Log what we found in annotations
	if portID != "" || floatingIPID != "" {
		logger.Info("Found resource IDs in service annotations, will force remove",
			"portID", portID,
			"floatingIPID", floatingIPID)
	}

	// Try to delete floating IPs, but continue even if they don't exist
	if floatingIPID != "" {
		logger.Info("Attempting to delete floating IP", "floatingIPID", floatingIPID)
		err := h.OpenStackClient.DeleteFloatingIP(ctx, floatingIPID)
		if err != nil && !openstack.IsNotFoundError(err) {
			logger.Error(err, "Failed to delete floating IP, but continuing")
		}
	}

	// Try to delete ports, but continue even if they don't exist
	if portID != "" {
		logger.Info("Attempting to delete port", "portID", portID)
		err := h.OpenStackClient.DeletePort(ctx, portID)
		if err != nil && !openstack.IsNotFoundError(err) {
			logger.Error(err, "Failed to delete port, but continuing")
		}
	}

	// If the service is being deleted, only try to remove finalizers
	if !svc.DeletionTimestamp.IsZero() {
		logger.Info("Service is being deleted, removing finalizer without updating annotations")
		return false, nil
	}

	// For non-terminating services, try to clean annotations
	// Create patch to remove both annotations at once
	annotations := map[string]string{
		AnnotationPortID:       "",
		AnnotationFloatingIPID: "",
	}

	logger.Info("Updating service annotations to remove resource references")
	if err := updateMultipleServiceAnnotations(ctx, h.Client, svc, annotations); err != nil {
		logger.Error(err, "Failed to update service annotations")
		needsRequeue = true
	}

	// Check if we need to requeue
	if needsRequeue {
		logger.Info("Resource cleanup not complete, will requeue")
		return true, nil
	}

	logger.Info("All resources cleaned up or annotations removed successfully")
	return false, nil
}

// identifyFloatingIPsToDelete identifies floating IPs that should be deleted
func (h *ResourceCleanupHandler) identifyFloatingIPsToDelete(
	allFloatingIPs []floatingips.FloatingIP,
	svc *corev1.Service,
	portID string,
	floatingIPID string,
) []floatingips.FloatingIP {
	logger := log.FromContext(context.Background()).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)
	var toDelete []floatingips.FloatingIP

	for _, fip := range allFloatingIPs {
		shouldDelete := false
		reason := ""

		// Case 1: Floating IP is referenced in service annotations
		if fip.ID == floatingIPID {
			shouldDelete = true
			reason = "annotation match"
		}

		// Case 2: Floating IP is attached to a port owned by this service
		if portID != "" && fip.PortID == portID {
			shouldDelete = true
			reason = "port association"
		}

		// Case 3: Check description for service identifier
		serviceIdentifier := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
		if strings.Contains(fip.Description, serviceIdentifier) {
			shouldDelete = true
			reason = "description match"
		}

		// Case 4: Check for "by OpenStack LB Operator" in the description
		if strings.Contains(fip.Description, "by OpenStack LB Operator") {
			// Additional check to avoid deleting resources for other services
			if strings.Contains(fip.Description, svc.Name) {
				shouldDelete = true
				reason = "operator description match"
			}
		}

		// Add to deletion list if any criteria matched
		if shouldDelete {
			logger.V(1).Info("Identified floating IP for deletion",
				"floatingIPID", fip.ID,
				"reason", reason,
				"address", fip.FloatingIP)
			toDelete = append(toDelete, fip)
		}
	}

	return toDelete
}

// identifyPortsToDelete identifies ports that should be deleted
func (h *ResourceCleanupHandler) identifyPortsToDelete(
	allPorts []ports.Port,
	svc *corev1.Service,
	portID string,
) []ports.Port {
	logger := log.FromContext(context.Background()).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)
	var toDelete []ports.Port

	for _, port := range allPorts {
		shouldDelete := false
		reason := ""

		// Case 1: Port is referenced in service annotations
		if port.ID == portID {
			shouldDelete = true
			reason = "annotation match"
		}

		// Case 2: Port name follows our naming convention for this service
		serviceIdentifier := fmt.Sprintf("k8s-lb-%s-%s", svc.Namespace, svc.Name)
		if port.Name == serviceIdentifier {
			shouldDelete = true
			reason = "name match"
		}

		// Case 3: Check description for service identifier
		serviceIdentifier = fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
		if strings.Contains(port.Description, serviceIdentifier) {
			shouldDelete = true
			reason = "description match"
		}

		// Case 4: Check for "by OpenStack LB Operator" in the description
		if strings.Contains(port.Description, "by OpenStack LB Operator") {
			// Additional check to avoid deleting resources for other services
			if strings.Contains(port.Description, svc.Name) {
				shouldDelete = true
				reason = "operator description match"
			}
		}

		// Add to deletion list if any criteria matched
		if shouldDelete {
			logger.V(1).Info("Identified port for deletion",
				"portID", port.ID,
				"reason", reason,
				"name", port.Name)
			toDelete = append(toDelete, port)
		}
	}

	return toDelete
}

// HandleServiceFinalization handles the finalization of a service that is being deleted
func (h *ResourceCleanupHandler) HandleServiceFinalization(ctx context.Context, svc *corev1.Service) (bool, error) {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)

	// Skip if the service doesn't have our finalizer
	if !controllerutil.ContainsFinalizer(svc, ServiceFinalizer) {
		return false, nil
	}

	logger.Info("Handling finalization for service being deleted")

	// Clean up resources
	needsRequeue, err := h.CleanupResourcesForService(ctx, svc)
	if err != nil {
		logger.Error(err, "Error during resource cleanup")
		return true, err
	}

	// If cleanup is not complete, requeue
	if needsRequeue {
		return true, nil
	}

	// All resources cleaned up, remove finalizer
	svcCopy := svc.DeepCopy()
	controllerutil.RemoveFinalizer(svcCopy, ServiceFinalizer)
	if err := updateResourceWithRetry(ctx, h.Client, svcCopy); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return true, err
	}

	logger.Info("Successfully removed finalizer, service can now be deleted")
	return false, nil
}
