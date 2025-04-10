package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	openstackv1alpha1 "github.com/jacero-io/openstack-lb-operator/api/v1alpha1"
	"github.com/jacero-io/openstack-lb-operator/pkg/openstack"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Condition types
	ConditionTypeReady             = "Ready"
	ConditionTypeNetworkDetected   = "NetworkDetected"
	ConditionTypePortCreated       = "PortCreated"
	ConditionTypeFloatingIPCreated = "FloatingIPCreated"

	// Finalizer
	OpenstackLBFinalizer = "openstack.jacero.io/finalizer"
	ServiceFinalizer     = "openstack.jacero.io/service-finalizer"

	// Annotations and labels
	AnnotationLoadBalancerClass          = "service.kubernetes.io/loadbalancer-class"
	AnnotationPortID                     = "openstack.jacero.io/port-id"
	AnnotationFloatingIPID               = "openstack.jacero.io/floating-ip-id"
	AnnotationCredentialsSecretName      = "openstack.jacero.io/credentials-secret-name"
	AnnotationCredentialsSecretNamespace = "openstack.jacero.io/credentials-secret-namespace"

	LabelManagedBy  = "app.kubernetes.io/managed-by"
	LabelCreatedFor = "openstack.jacero.io/created-for"
)

// Retry settings
var (
	RetryBackoff = wait.Backoff{
		Steps:    5,
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
	}
)

// getOpenStackClient creates a new OpenStack client using credentials from the secret
func (r *OpenStackLoadBalancerReconciler) getOpenStackClient(ctx context.Context, lb *openstackv1alpha1.OpenStackLoadBalancer) (openstack.Client, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("Getting OpenStack client using credentials",
		"secretName", lb.Spec.ApplicationCredentialSecretRef.Name,
		"secretNamespace", lb.Spec.ApplicationCredentialSecretRef.Namespace)

	return openstack.NewClient(
		ctx,
		r.Client,
		lb.Spec.ApplicationCredentialSecretRef.Name,
		lb.Spec.ApplicationCredentialSecretRef.Namespace,
	)
}

// isServiceMatchingLBClass checks if a service matches the specified LoadBalancer class
func isServiceMatchingLBClass(svc *corev1.Service, className string) bool {
	// Check for LoadBalancer class in spec (K8s 1.24+)
	if svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == className {
		return true
	}

	// Check for the annotation (for backward compatibility or custom implementations)
	if value, exists := svc.Annotations[AnnotationLoadBalancerClass]; exists && value == className {
		return true
	}

	return false
}

// updateServiceAnnotationWithRetry updates an annotation on a service with retry logic
func updateServiceAnnotationWithRetry(
	ctx context.Context,
	c client.Client,
	svc *corev1.Service,
	key string,
	value string,
) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Updating service annotation",
		"service", svc.Name,
		"namespace", svc.Namespace,
		"key", key)

	return wait.ExponentialBackoff(RetryBackoff, func() (bool, error) {
		// Get the latest version of the service
		currentSvc := &corev1.Service{}
		if err := c.Get(ctx, client.ObjectKey{Name: svc.Name, Namespace: svc.Namespace}, currentSvc); err != nil {
			if apierrors.IsNotFound(err) {
				// Service doesn't exist anymore, no need to update
				return true, nil
			}
			// Other errors should be retried
			logger.Error(err, "Failed to get service for annotation update, will retry",
				"service", svc.Name,
				"namespace", svc.Namespace)
			return false, nil
		}

		// Create a copy to avoid modifying the cache
		svcCopy := currentSvc.DeepCopy()

		// Ensure annotations map exists
		if svcCopy.Annotations == nil {
			svcCopy.Annotations = make(map[string]string)
		}

		// Set or update the annotation
		svcCopy.Annotations[key] = value

		// Update the service
		if err := c.Update(ctx, svcCopy); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict error means someone else modified the object
				// We'll retry with the latest version
				logger.V(1).Info("Conflict updating service annotation, will retry",
					"service", svc.Name,
					"namespace", svc.Namespace,
					"key", key)
				return false, nil
			}
			// Other errors should be returned
			return false, err
		}

		// Update succeeded
		logger.V(1).Info("Successfully updated service annotation",
			"service", svc.Name,
			"namespace", svc.Namespace,
			"key", key)
		return true, nil
	})
}

// updateMultipleServiceAnnotations updates multiple annotations on a service in a single update operation
func updateMultipleServiceAnnotations(
	ctx context.Context,
	c client.Client,
	svc *corev1.Service,
	annotations map[string]string,
) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Updating multiple service annotations",
		"service", svc.Name,
		"namespace", svc.Namespace)

	return wait.ExponentialBackoff(RetryBackoff, func() (bool, error) {
		// Get the latest version of the service
		currentSvc := &corev1.Service{}
		if err := c.Get(ctx, client.ObjectKey{Name: svc.Name, Namespace: svc.Namespace}, currentSvc); err != nil {
			if apierrors.IsNotFound(err) {
				// Service doesn't exist anymore, no need to update
				return true, nil
			}
			// Other errors should be retried
			logger.Error(err, "Failed to get service for annotation updates, will retry",
				"service", svc.Name,
				"namespace", svc.Namespace)
			return false, nil
		}

		// Create a copy to avoid modifying the cache
		svcCopy := currentSvc.DeepCopy()

		// Ensure annotations map exists
		if svcCopy.Annotations == nil {
			svcCopy.Annotations = make(map[string]string)
		}

		// Apply all annotation changes
		for key, value := range annotations {
			svcCopy.Annotations[key] = value
		}

		// Update the service
		if err := c.Update(ctx, svcCopy); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict error means someone else modified the object
				// We'll retry with the latest version
				logger.V(1).Info("Conflict updating service annotations, will retry",
					"service", svc.Name,
					"namespace", svc.Namespace)
				return false, nil
			}
			// Other errors should be returned
			return false, err
		}

		// Update succeeded
		logger.V(1).Info("Successfully updated service annotations",
			"service", svc.Name,
			"namespace", svc.Namespace)
		return true, nil
	})
}

// updateStatusWithRetry updates the status of an OpenStackLoadBalancer with retry logic
func updateStatusWithRetry(
	ctx context.Context,
	c client.Client,
	lb *openstackv1alpha1.OpenStackLoadBalancer,
) error {
	logger := log.FromContext(ctx)

	return wait.ExponentialBackoff(RetryBackoff, func() (bool, error) {
		// Get the latest version of the resource
		current := &openstackv1alpha1.OpenStackLoadBalancer{}
		if err := c.Get(ctx, client.ObjectKey{Name: lb.Name, Namespace: lb.Namespace}, current); err != nil {
			if apierrors.IsNotFound(err) {
				// Resource doesn't exist anymore, no need to update
				return true, nil
			}
			// Other errors should be retried
			logger.Error(err, "Failed to get OpenStackLoadBalancer for status update, will retry",
				"name", lb.Name,
				"namespace", lb.Namespace)
			return false, nil
		}

		// Create a copy to avoid modifying the cache
		lbCopy := current.DeepCopy()

		// Copy status fields from the original lb to the latest version
		lbCopy.Status.Conditions = lb.Status.Conditions
		lbCopy.Status.LBNetworkID = lb.Status.LBNetworkID
		lbCopy.Status.LBSubnetID = lb.Status.LBSubnetID
		lbCopy.Status.Ready = lb.Status.Ready

		// Update the status
		if err := c.Status().Update(ctx, lbCopy); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict error means someone else modified the object
				// We'll retry with the latest version
				logger.V(1).Info("Conflict updating status, will retry",
					"name", lb.Name,
					"namespace", lb.Namespace)
				return false, nil
			}
			// Other errors should be returned
			return false, err
		}

		// Update succeeded
		logger.V(1).Info("Successfully updated status",
			"name", lb.Name,
			"namespace", lb.Namespace)

		// Copy back the updated status to the original object
		lb.Status = lbCopy.Status

		return true, nil
	})
}

// updateResourceWithRetry updates a Kubernetes resource with retry logic
func updateResourceWithRetry(
	ctx context.Context,
	c client.Client,
	obj client.Object,
) error {
	logger := log.FromContext(ctx)
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", obj)
	}

	return wait.ExponentialBackoff(RetryBackoff, func() (bool, error) {
		// Get the latest version of the resource
		current := obj.DeepCopyObject().(client.Object)
		if err := c.Get(ctx, client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}, current); err != nil {
			if apierrors.IsNotFound(err) {
				// Resource doesn't exist anymore, no need to update
				return true, nil
			}
			// Other errors should be retried
			logger.Error(err, "Failed to get resource for update, will retry",
				"kind", kind,
				"name", obj.GetName(),
				"namespace", obj.GetNamespace())
			return false, nil
		}

		// Set resource version to ensure we're updating the latest version
		obj.SetResourceVersion(current.GetResourceVersion())

		// Update the resource
		if err := c.Update(ctx, obj); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict error means someone else modified the object
				// We'll retry with the latest version
				logger.V(1).Info("Conflict updating resource, will retry",
					"kind", kind,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace())
				return false, nil
			}
			// Other errors should be returned
			return false, err
		}

		// Update succeeded
		logger.V(1).Info("Successfully updated resource",
			"kind", kind,
			"name", obj.GetName(),
			"namespace", obj.GetNamespace())
		return true, nil
	})
}

// setCondition sets a condition on the OpenStackLoadBalancer status
func setCondition(lb *openstackv1alpha1.OpenStackLoadBalancer, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, condition := range lb.Status.Conditions {
		if condition.Type == conditionType {
			// Update existing condition
			if condition.Status != status || condition.Reason != reason || condition.Message != message {
				lb.Status.Conditions[i] = metav1.Condition{
					Type:               conditionType,
					Status:             status,
					LastTransitionTime: now,
					Reason:             reason,
					Message:            message,
				}
			}
			return
		}
	}

	// Create new condition
	lb.Status.Conditions = append(lb.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

// hasAnnotation checks if a service has a specific annotation with a non-empty value
func hasAnnotation(svc *corev1.Service, key string) bool {
	if svc.Annotations == nil {
		return false
	}
	value, exists := svc.Annotations[key]
	return exists && value != ""
}

// extractResourceIDsFromAnnotations gets resource IDs from service annotations
func extractResourceIDsFromAnnotations(svc *corev1.Service) (portID, floatingIPID string) {
	if svc.Annotations != nil {
		portID = svc.Annotations[AnnotationPortID]
		floatingIPID = svc.Annotations[AnnotationFloatingIPID]
	}
	return
}

// forceFinalizerRemoval is a last-resort method to remove a finalizer from a service
// that's stuck in Terminating state. It uses a strategic merge patch to only update
// the finalizers field, avoiding conflicts with other controllers.
func forceFinalizerRemoval(ctx context.Context, c client.Client, svc *corev1.Service) error {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
	)

	// Don't try if service isn't in terminating state
	if svc.DeletionTimestamp.IsZero() {
		return nil
	}

	logger.Info("Attempting to force remove finalizer from terminating service")

	// Use strategic merge patch to only modify the finalizers field
	patchBytes := []byte(`{"metadata":{"finalizers":null}}`)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return c.Patch(ctx, svc, client.RawPatch(types.StrategicMergePatchType, patchBytes))
	})

	if err != nil {
		logger.Error(err, "Failed to force remove finalizer")
		return err
	}

	logger.Info("Successfully forced finalizer removal")
	return nil
}

// shouldForceCleanup determines if we should force cleanup based on service state
// and multiple failed attempts
func shouldForceCleanup(svc *corev1.Service) bool {
	// If service is terminating and has our finalizer, we should force cleanup
	if !svc.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(svc, ServiceFinalizer) {
		// Additionally check if service has annotations suggesting OpenStack resources
		// that can't be found in OpenStack anymore
		return hasAnnotation(svc, AnnotationPortID) || hasAnnotation(svc, AnnotationFloatingIPID)
	}
	return false
}

func (h *ResourceCleanupHandler) HandleServiceFinalizationWithForce(ctx context.Context, svc *corev1.Service) (bool, error) {
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

		// Check if we should force finalizer removal due to being stuck
		if shouldForceCleanup(svc) {
			logger.Info("Detected stuck service cleanup, forcing finalizer removal")
			if err := forceFinalizerRemoval(ctx, h.Client, svc); err != nil {
				return true, err
			}
			return false, nil
		}

		return true, err
	}

	// If cleanup is still in progress, check if we need to force it
	if needsRequeue {
		// Check if we should force finalizer removal
		if shouldForceCleanup(svc) {
			logger.Info("Cleanup incomplete but service is terminating, forcing finalizer removal")
			if err := forceFinalizerRemoval(ctx, h.Client, svc); err != nil {
				return true, err
			}
			return false, nil
		}
		return true, nil
	}

	// All resources cleaned up, remove finalizer normally
	svcCopy := svc.DeepCopy()
	controllerutil.RemoveFinalizer(svcCopy, ServiceFinalizer)
	if err := updateResourceWithRetry(ctx, h.Client, svcCopy); err != nil {
		logger.Error(err, "Failed to remove finalizer")

		// If the normal method fails, try forcing it
		if strings.Contains(err.Error(), "conflict") || !svc.DeletionTimestamp.IsZero() {
			if err := forceFinalizerRemoval(ctx, h.Client, svc); err != nil {
				return true, err
			}
		} else {
			return true, err
		}
	}

	logger.Info("Successfully removed finalizer, service can now be deleted")
	return false, nil
}

// updateServiceExternalIPs updates the service's externalIPs field to include the floating IP
func updateServiceExternalIPs(
	ctx context.Context,
	c client.Client,
	svc *corev1.Service,
	floatingIPAddress string,
) error {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
		"floatingIP", floatingIPAddress,
	)

	return wait.ExponentialBackoff(RetryBackoff, func() (bool, error) {
		// Get the latest version of the service
		currentSvc := &corev1.Service{}
		if err := c.Get(ctx, client.ObjectKey{Name: svc.Name, Namespace: svc.Namespace}, currentSvc); err != nil {
			if apierrors.IsNotFound(err) {
				// Service doesn't exist anymore, no need to update
				return true, nil
			}
			// Other errors should be retried
			logger.Error(err, "Failed to get service for externalIPs update, will retry")
			return false, nil
		}

		// Create a copy to avoid modifying the cache
		svcCopy := currentSvc.DeepCopy()

		// Check if IP is already in externalIPs
		ipExists := false
		for _, ip := range svcCopy.Spec.ExternalIPs {
			if ip == floatingIPAddress {
				ipExists = true
				break
			}
		}

		// If IP doesn't exist in the list, add it
		if !ipExists {
			svcCopy.Spec.ExternalIPs = append(svcCopy.Spec.ExternalIPs, floatingIPAddress)

			// Update the service
			if err := c.Update(ctx, svcCopy); err != nil {
				if apierrors.IsConflict(err) {
					// Conflict error means someone else modified the object
					// We'll retry with the latest version
					logger.V(1).Info("Conflict updating service externalIPs, will retry")
					return false, nil
				}
				// Other errors should be returned
				return false, err
			}

			logger.Info("Successfully added floating IP to service externalIPs")
		} else {
			logger.V(1).Info("Floating IP already exists in service externalIPs")
		}

		return true, nil
	})
}
