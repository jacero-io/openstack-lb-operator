package controller

import (
	"context"
	"fmt"
	"strings"

	openstackv1alpha1 "github.com/jacero-io/openstack-lb-operator/api/v1alpha1"
	"github.com/jacero-io/openstack-lb-operator/pkg/openstack"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// findOpenStackLBsForService maps from a Service to potential OpenStackLoadBalancer objects
func (r *OpenStackLoadBalancerReconciler) findOpenStackLBsForService(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)
	svc, ok := obj.(*corev1.Service)
	if !ok {
		logger.Error(nil, "Expected a Service but got something else")
		return nil
	}

	// Find all OpenStackLoadBalancer objects
	lbs := &openstackv1alpha1.OpenStackLoadBalancerList{}
	if err := r.List(ctx, lbs); err != nil {
		logger.Error(err, "Failed to list OpenStackLoadBalancers")
		return nil
	}

	var requests []reconcile.Request

	// Check if this service is managed by our operator (either by class or finalizer)
	isManagedService := false

	// Check if service has our finalizer or annotations
	if controllerutil.ContainsFinalizer(svc, ServiceFinalizer) ||
		(svc.Annotations != nil && (svc.Annotations[AnnotationPortID] != "" ||
			svc.Annotations[AnnotationFloatingIPID] != "")) {
		isManagedService = true
	}

	// Check if it matches any LB class
	for _, lb := range lbs.Items {
		if isServiceMatchingLBClass(svc, lb.Spec.ClassName) {
			isManagedService = true
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      lb.Name,
					Namespace: lb.Namespace,
				},
			})
		}
	}

	// If this is a service we care about (managed or matching),
	// also add a reconcile request for a special Service handler
	if isManagedService {
		// Create a special reconcile request just for handling this service
		// We'll use a consistent name format for the fake OpenStackLoadBalancer
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      fmt.Sprintf("service-handler-%s", svc.Name),
				Namespace: svc.Namespace,
			},
		})
	}

	return requests
}

// ensureServiceFinalizer adds our finalizer to the service if it doesn't already have it
func (r *OpenStackLoadBalancerReconciler) ensureServiceFinalizer(ctx context.Context, svc *corev1.Service) error {
	logger := log.FromContext(ctx)

	// Skip if the service already has our finalizer
	if controllerutil.ContainsFinalizer(svc, ServiceFinalizer) {
		return nil
	}

	// Add our finalizer
	svcCopy := svc.DeepCopy()
	controllerutil.AddFinalizer(svcCopy, ServiceFinalizer)

	if err := updateResourceWithRetry(ctx, r.Client, svcCopy); err != nil {
		logger.Error(err, "Failed to add finalizer to service",
			"service", svc.Name,
			"namespace", svc.Namespace)
		return err
	}

	logger.Info("Added finalizer to service",
		"service", svc.Name,
		"namespace", svc.Namespace)
	return nil
}

// updateServiceWithCredentials stores credential references in service annotations
func (r *OpenStackLoadBalancerReconciler) updateServiceWithCredentials(ctx context.Context, svc *corev1.Service, lb *openstackv1alpha1.OpenStackLoadBalancer) error {
	logger := log.FromContext(ctx)
	annotations := map[string]string{
		AnnotationCredentialsSecretName:      lb.Spec.ApplicationCredentialSecretRef.Name,
		AnnotationCredentialsSecretNamespace: lb.Spec.ApplicationCredentialSecretRef.Namespace,
	}

	logger.Info("Storing credentials reference in service",
		"service", svc.Name,
		"secretName", lb.Spec.ApplicationCredentialSecretRef.Name)

	return updateMultipleServiceAnnotations(ctx, r.Client, svc, annotations)
}

// reconcileService handles Service objects that are being created, updated, or deleted
func (r *OpenStackLoadBalancerReconciler) reconcileService(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Extract service name from the request name (which has format "service-handler-{svcName}")
	nameParts := strings.Split(req.Name, "service-handler-")
	if len(nameParts) != 2 {
		// Not a service handler request
		return ctrl.Result{}, nil
	}

	serviceName := nameParts[1]
	serviceNamespace := req.Namespace

	// Get the service
	svc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: serviceNamespace}, svc); err != nil {
		if apierrors.IsNotFound(err) {
			// Service doesn't exist, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get service")
		return ctrl.Result{}, err
	}

	// If service is being deleted
	if !svc.DeletionTimestamp.IsZero() {
		// Get OpenStack client using stored credentials if available
		osClient, err := r.getOpenStackClientFromService(ctx, svc)
		if err != nil {
			logger.Error(err, "Failed to create OpenStack client for service")
			// If we can't get credentials, we still need to remove the finalizer
			// to avoid blocking service deletion
			if controllerutil.ContainsFinalizer(svc, ServiceFinalizer) {
				svcCopy := svc.DeepCopy()
				controllerutil.RemoveFinalizer(svcCopy, ServiceFinalizer)
				if updateErr := updateResourceWithRetry(ctx, r.Client, svcCopy); updateErr != nil {
					logger.Error(updateErr, "Failed to remove finalizer")
					return ctrl.Result{RequeueAfter: RequeueAfter}, updateErr
				}
				logger.Info("Removed finalizer without cleaning resources - manual cleanup may be needed")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{RequeueAfter: RequeueAfter}, err
		}

		// Create a cleanup handler and handle finalization
		cleanupHandler := NewResourceCleanupHandler(r.Client, osClient)
		needsRequeue, err := cleanupHandler.HandleServiceFinalization(ctx, svc)
		if err != nil {
			return ctrl.Result{RequeueAfter: RequeueAfter}, err
		}
		if needsRequeue {
			return ctrl.Result{RequeueAfter: RequeueAfter}, nil
		}
		return ctrl.Result{}, nil
	}

	// For non-deleted services, ensure finalizer if needed
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		// Find all OpenStackLoadBalancer objects
		lbs := &openstackv1alpha1.OpenStackLoadBalancerList{}
		if err := r.List(ctx, lbs); err != nil {
			logger.Error(err, "Failed to list OpenStackLoadBalancers")
			return ctrl.Result{RequeueAfter: RequeueAfter}, err
		}

		// Check if the service matches any LB class
		var matchingLBFound bool
		var matchingLB *openstackv1alpha1.OpenStackLoadBalancer

		for i, lb := range lbs.Items {
			if isServiceMatchingLBClass(svc, lb.Spec.ClassName) {
				matchingLBFound = true
				matchingLB = &lbs.Items[i]
				break
			}
		}

		// Also check if there are annotations indicating we manage this service
		hasOurAnnotations := hasAnnotation(svc, AnnotationPortID) || hasAnnotation(svc, AnnotationFloatingIPID)

		// Add finalizer if the service matches an LB class or has our annotations
		if (matchingLBFound || hasOurAnnotations) && !controllerutil.ContainsFinalizer(svc, ServiceFinalizer) {
			if err := r.ensureServiceFinalizer(ctx, svc); err != nil {
				return ctrl.Result{RequeueAfter: RequeueAfter}, err
			}

			// If we found a matching LB, store the credential reference in the service
			if matchingLB != nil {
				if err := r.updateServiceWithCredentials(ctx, svc, matchingLB); err != nil {
					logger.Error(err, "Failed to update service with credentials")
					return ctrl.Result{RequeueAfter: RequeueAfter}, err
				}
			}
		}
	}

	return ctrl.Result{}, nil
}

// getOpenStackClientFromService tries to get an OpenStack client using credentials
// stored in the service annotations first, falling back to the old method if not available
func (r *OpenStackLoadBalancerReconciler) getOpenStackClientFromService(ctx context.Context, svc *corev1.Service) (openstack.Client, error) {
	logger := log.FromContext(ctx)

	// First, try to use credentials stored in service annotations
	if svc.Annotations != nil {
		secretName := svc.Annotations[AnnotationCredentialsSecretName]
		secretNamespace := svc.Annotations[AnnotationCredentialsSecretNamespace]

		if secretName != "" && secretNamespace != "" {
			logger.Info("Using credentials from service annotations",
				"secretName", secretName,
				"secretNamespace", secretNamespace)

			return openstack.NewClient(ctx, r.Client, secretName, secretNamespace)
		}
	}

	// Fall back to previous method
	return r.getOpenStackClientForService(ctx, svc)
}

// getOpenStackClientForService tries to find an OpenStackLoadBalancer that matches the service
// and uses its credentials to create an OpenStack client
func (r *OpenStackLoadBalancerReconciler) getOpenStackClientForService(ctx context.Context, svc *corev1.Service) (openstack.Client, error) {
	logger := log.FromContext(ctx)

	// Find all OpenStackLoadBalancer objects
	lbs := &openstackv1alpha1.OpenStackLoadBalancerList{}
	if err := r.List(ctx, lbs); err != nil {
		logger.Error(err, "Failed to list OpenStackLoadBalancers")
		return nil, err
	}

	// Try to find a matching LB first
	for _, lb := range lbs.Items {
		if isServiceMatchingLBClass(svc, lb.Spec.ClassName) {
			osClient, err := r.getOpenStackClient(ctx, &lb)
			if err == nil {
				return osClient, nil
			}
			logger.Error(err, "Failed to create OpenStack client using matched LB", "loadbalancer", lb.Name)
		}
	}

	// If no matching LB or error getting client, try any LB
	if len(lbs.Items) > 0 {
		osClient, err := r.getOpenStackClient(ctx, &lbs.Items[0])
		if err != nil {
			logger.Error(err, "Failed to create OpenStack client using first available LB")
			return nil, err
		}
		return osClient, nil
	}

	return nil, fmt.Errorf("no OpenStackLoadBalancer found to get credentials")
}
