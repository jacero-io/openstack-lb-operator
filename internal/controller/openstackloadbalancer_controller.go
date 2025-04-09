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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// RequeueAfter defines how long to wait before requeuing
const RequeueAfter = 30 * time.Second

// OpenStackLoadBalancerReconciler reconciles a OpenStackLoadBalancer object
type OpenStackLoadBalancerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=openstack.jacero.io,resources=openstackloadbalancers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openstack.jacero.io,resources=openstackloadbalancers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openstack.jacero.io,resources=openstackloadbalancers/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile handles the reconciliation loop for OpenStackLoadBalancer resources
func (r *OpenStackLoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("openstackloadbalancer", req.NamespacedName)

	// Check if this is a special service handler request
	if strings.HasPrefix(req.Name, "service-handler-") {
		return r.reconcileService(ctx, req)
	}

	logger.Info("Reconciling OpenStackLoadBalancer")

	// Fetch the OpenStackLoadBalancer instance
	lb := &openstackv1alpha1.OpenStackLoadBalancer{}
	if err := r.Get(ctx, req.NamespacedName, lb); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, likely already deleted
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get OpenStackLoadBalancer")
		return ctrl.Result{}, err
	}

	// Initialize status conditions if not set
	if lb.Status.Conditions == nil {
		lb.Status.Conditions = []metav1.Condition{}
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(lb, OpenstackLBFinalizer) && lb.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(lb, OpenstackLBFinalizer)
		if err := updateResourceWithRetry(ctx, r.Client, lb); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return early to avoid further processing in this reconciliation
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !lb.DeletionTimestamp.IsZero() {
		logger.Info("Handling deletion of OpenStackLoadBalancer")
		return r.handleDeletion(ctx, lb)
	}

	// Get OpenStack client
	osClient, err := r.getOpenStackClient(ctx, lb)
	if err != nil {
		logger.Error(err, "Failed to create OpenStack client")
		setCondition(lb, ConditionTypeReady, metav1.ConditionFalse, "OpenStackClientError", err.Error())
		return ctrl.Result{}, updateStatusWithRetry(ctx, r.Client, lb)
	}

	// Look for services that match this OpenStackLoadBalancer's class
	svcs := &corev1.ServiceList{}
	if err := r.List(ctx, svcs); err != nil {
		logger.Error(err, "Failed to list services")
		return ctrl.Result{}, err
	}

	// Process each matching service
	var processedAtLeastOne bool
	var lastError error
	var needsRequeue bool

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		if !isServiceMatchingLBClass(svc, lb.Spec.ClassName) || svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}

		// Skip if service is being deleted (handled by service reconciler)
		if !svc.DeletionTimestamp.IsZero() {
			continue
		}

		// Skip if service doesn't have an IP assigned yet
		if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
			logger.Info("Service has no LoadBalancer IP yet, skipping",
				"service", svc.Name, "namespace", svc.Namespace)
			continue
		}

		serviceIP := svc.Status.LoadBalancer.Ingress[0].IP
		logger.Info("Processing service",
			"service", svc.Name, "namespace", svc.Namespace, "ip", serviceIP)

		// Process this service
		requeue, err := r.processService(ctx, lb, svc, osClient, serviceIP)
		if err != nil {
			logger.Error(err, "Error processing service",
				"service", svc.Name, "namespace", svc.Namespace)
			lastError = err
		}

		if requeue {
			needsRequeue = true
		}

		processedAtLeastOne = true
	}

	if !processedAtLeastOne {
		logger.Info("No matching services with LoadBalancer IP found for class", "class", lb.Spec.ClassName)
		setCondition(lb, ConditionTypeReady, metav1.ConditionFalse, "NoMatchingService",
			fmt.Sprintf("No service found with LoadBalancer type and class %s", lb.Spec.ClassName))
	} else if lastError != nil {
		setCondition(lb, ConditionTypeReady, metav1.ConditionFalse, "ProcessingError", lastError.Error())
	} else {
		setCondition(lb, ConditionTypeReady, metav1.ConditionTrue, "ResourcesReady", "All OpenStack resources are ready")
		lb.Status.Ready = true
	}

	// Update the status with retries
	if err := updateStatusWithRetry(ctx, r.Client, lb); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	if needsRequeue {
		return ctrl.Result{RequeueAfter: RequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// processService handles the reconciliation for a single service
func (r *OpenStackLoadBalancerReconciler) processService(
	ctx context.Context,
	lb *openstackv1alpha1.OpenStackLoadBalancer,
	svc *corev1.Service,
	osClient openstack.Client,
	serviceIP string,
) (bool, error) {
	logger := log.FromContext(ctx).WithValues(
		"service", svc.Name,
		"namespace", svc.Namespace,
		"ip", serviceIP,
	)

	// Ensure the service has our finalizer
	if !controllerutil.ContainsFinalizer(svc, ServiceFinalizer) && svc.DeletionTimestamp.IsZero() {
		svcCopy := svc.DeepCopy()
		controllerutil.AddFinalizer(svcCopy, ServiceFinalizer)

		if err := updateResourceWithRetry(ctx, r.Client, svcCopy); err != nil {
			logger.Error(err, "Failed to add finalizer to service")
			return true, err
		}

		logger.Info("Added finalizer to service")
		// Return early to avoid further processing in this reconciliation
		return true, nil
	}

	// Ensure the service has our credential references
	if !hasAnnotation(svc, AnnotationCredentialsSecretName) ||
		!hasAnnotation(svc, AnnotationCredentialsSecretNamespace) {
		annotationsToUpdate := map[string]string{
			AnnotationCredentialsSecretName:      lb.Spec.ApplicationCredentialSecretRef.Name,
			AnnotationCredentialsSecretNamespace: lb.Spec.ApplicationCredentialSecretRef.Namespace,
		}
		if err := updateMultipleServiceAnnotations(ctx, r.Client, svc, annotationsToUpdate); err != nil {
			logger.Error(err, "Failed to update service with credential annotations")
			return true, err
		}
		logger.Info("Added credential references to service annotations")
		// Continue processing in this reconciliation
	}

	// Don't proceed with resource creation if the service is being deleted
	if !svc.DeletionTimestamp.IsZero() {
		logger.Info("Service is being deleted, skipping resource creation")
		return false, nil
	}

	// Detect network and subnet for the service IP if not already cached
	if lb.Status.LBNetworkID == "" || lb.Status.LBSubnetID == "" {
		networkID, subnetID, err := osClient.DetectNetworkForIP(ctx, serviceIP)
		if err != nil {
			logger.Error(err, "Failed to detect network for service IP")
			setCondition(lb, ConditionTypeNetworkDetected, metav1.ConditionFalse, "NetworkDetectionFailed", err.Error())
			return true, err
		}

		lb.Status.LBNetworkID = networkID
		lb.Status.LBSubnetID = subnetID
		setCondition(lb, ConditionTypeNetworkDetected, metav1.ConditionTrue, "NetworkDetected",
			fmt.Sprintf("Detected network %s and subnet %s for IP %s", networkID, subnetID, serviceIP))
	}

	// Check if port already exists for this service (from annotation)
	var portID string
	if val, exists := svc.Annotations[AnnotationPortID]; exists && val != "" {
		portID = val

		// Verify port still exists in OpenStack
		exists, err := osClient.GetPort(ctx, portID)
		if err != nil {
			return true, fmt.Errorf("failed to verify port %s: %w", portID, err)
		}

		if !exists {
			// Update annotation to remove the old port ID and reuse the return value
			err := updateServiceAnnotationWithRetry(ctx, r.Client, svc, AnnotationPortID, "")
			return true, err
		}
	}

	// Create port if it doesn't exist
	if portID == "" {
		portName := fmt.Sprintf("k8s-lb-%s-%s", svc.Namespace, svc.Name)
		description := fmt.Sprintf("Port created for Kubernetes service %s/%s by OpenStack LB Operator",
			svc.Namespace, svc.Name)

		newPortID, err := osClient.CreatePort(
			ctx,
			lb.Status.LBNetworkID,
			lb.Status.LBSubnetID,
			portName,
			serviceIP,
			description,
			svc.Namespace,
			svc.Name,
		)
		if err != nil {
			logger.Error(err, "Failed to create OpenStack port")
			setCondition(lb, ConditionTypePortCreated, metav1.ConditionFalse, "PortCreationFailed", err.Error())
			return true, err
		}

		portID = newPortID

		// Update the annotation on the service
		if err := updateServiceAnnotationWithRetry(ctx, r.Client, svc, AnnotationPortID, portID); err != nil {
			logger.Error(err, "Failed to update service with port ID annotation")
			return true, err
		}

		// Create an event for the service
		r.Recorder.Eventf(svc, corev1.EventTypeNormal, "PortCreated",
			"Created OpenStack port %s for service IP %s", portID, serviceIP)

		setCondition(lb, ConditionTypePortCreated, metav1.ConditionTrue, "PortCreated",
			fmt.Sprintf("Created port %s for IP %s", portID, serviceIP))
	}

	// Handle Floating IP if enabled
	if lb.Spec.CreateFloatingIP {
		// Check if floating IP already exists for this service (from annotation)
		var floatingIPID string
		if val, exists := svc.Annotations[AnnotationFloatingIPID]; exists && val != "" {
			floatingIPID = val

			// Verify floating IP still exists in OpenStack
			exists, err := osClient.GetFloatingIP(ctx, floatingIPID)
			if err != nil {
				return true, fmt.Errorf("failed to verify floating IP %s: %w", floatingIPID, err)
			}

			if !exists {
				// Update annotation to remove the old floating IP ID and reuse the return value
				err := updateServiceAnnotationWithRetry(ctx, r.Client, svc, AnnotationFloatingIPID, "")
				return true, err
			}
		}

		// Create floating IP if it doesn't exist
		if floatingIPID == "" {
			if lb.Spec.FloatingIPNetworkID == "" {
				err := fmt.Errorf("floatingIPNetworkID must be specified when createFloatingIP is true")
				logger.Error(err, "Missing floating IP network ID")
				setCondition(lb, ConditionTypeFloatingIPCreated, metav1.ConditionFalse, "MissingFloatingIPNetwork", err.Error())
				return true, err
			}

			// First, check if there's already a floating IP associated with this port in OpenStack
			existingFIPID, existingFIPAddress, err := osClient.GetFloatingIPByPortID(ctx, portID)
			if err != nil {
				logger.Error(err, "Failed to check for existing floating IPs")
				return true, err
			}

			if existingFIPID != "" {
				// Found existing floating IP, use it instead of creating a new one
				floatingIPID = existingFIPID

				// Update the annotation on the service
				if err := updateServiceAnnotationWithRetry(ctx, r.Client, svc, AnnotationFloatingIPID, floatingIPID); err != nil {
					logger.Error(err, "Failed to update service with floating IP ID annotation")
					return true, err
				}

				// Create an event for the service
				r.Recorder.Eventf(svc, corev1.EventTypeNormal, "FloatingIPFound",
					"Found existing OpenStack floating IP %s (%s) for service", floatingIPID, existingFIPAddress)

				setCondition(lb, ConditionTypeFloatingIPCreated, metav1.ConditionTrue, "FloatingIPFound",
					fmt.Sprintf("Found existing floating IP %s (%s) for port %s", floatingIPID, existingFIPAddress, portID))

				return false, nil
			}

			description := fmt.Sprintf("Floating IP for Kubernetes service %s/%s by OpenStack LB Operator",
				svc.Namespace, svc.Name)

			newFloatingIPID, floatingIPAddress, err := osClient.CreateFloatingIP(
				ctx,
				lb.Spec.FloatingIPNetworkID,
				portID,
				description,
				svc.Namespace,
				svc.Name,
			)
			if err != nil {
				logger.Error(err, "Failed to create OpenStack floating IP")
				setCondition(lb, ConditionTypeFloatingIPCreated, metav1.ConditionFalse, "FloatingIPCreationFailed", err.Error())
				return true, err
			}

			floatingIPID = newFloatingIPID

			// Update the annotation on the service
			if err := updateServiceAnnotationWithRetry(ctx, r.Client, svc, AnnotationFloatingIPID, floatingIPID); err != nil {
				logger.Error(err, "Failed to update service with floating IP ID annotation")
				return true, err
			}

			// Create an event for the service
			r.Recorder.Eventf(svc, corev1.EventTypeNormal, "FloatingIPCreated",
				"Created OpenStack floating IP %s (%s) for service", floatingIPID, floatingIPAddress)

			setCondition(lb, ConditionTypeFloatingIPCreated, metav1.ConditionTrue, "FloatingIPCreated",
				fmt.Sprintf("Created floating IP %s (%s) for port %s", floatingIPID, floatingIPAddress, portID))
		}
	}

	return false, nil
}

// handleDeletion handles the deletion of OpenStackLoadBalancer resources
func (r *OpenStackLoadBalancerReconciler) handleDeletion(ctx context.Context, lb *openstackv1alpha1.OpenStackLoadBalancer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling deletion of OpenStackLoadBalancer")

	// Get OpenStack client for cleanup
	osClient, err := r.getOpenStackClient(ctx, lb)
	if err != nil {
		logger.Error(err, "Failed to create OpenStack client for cleanup")
		// If we can't create client, we can't clean up resources
		// Remove finalizer anyway to allow resource deletion
		controllerutil.RemoveFinalizer(lb, OpenstackLBFinalizer)
		if updateErr := updateResourceWithRetry(ctx, r.Client, lb); updateErr != nil {
			logger.Error(updateErr, "Failed to remove finalizer")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	// Track if we need to requeue due to pending cleanups
	needsRequeue := false

	// Find all services that may have been managed by this loadbalancer
	svcs := &corev1.ServiceList{}
	if err := r.List(ctx, svcs, &client.ListOptions{}); err != nil {
		logger.Error(err, "Failed to list services for cleanup")
		return ctrl.Result{}, err
	}

	// Initialize the cleanup handler
	cleanupHandler := NewResourceCleanupHandler(r.Client, osClient)

	// Process each service that matches our LB class or has our annotations
	for i := range svcs.Items {
		svc := &svcs.Items[i]

		// Either it matches our LB class or it has our annotations
		if isServiceMatchingLBClass(svc, lb.Spec.ClassName) ||
			hasAnnotation(svc, AnnotationPortID) ||
			hasAnnotation(svc, AnnotationFloatingIPID) {

			logger.Info("Cleaning up resources for service",
				"service", svc.Name,
				"namespace", svc.Namespace)

			// Check if service is terminating - if so, use force method
			if !svc.DeletionTimestamp.IsZero() {
				needs, err := cleanupHandler.HandleServiceFinalizationWithForce(ctx, svc)
				if err != nil {
					logger.Error(err, "Error during forced cleanup for terminating service",
						"service", svc.Name,
						"namespace", svc.Namespace)
					needsRequeue = true
				} else if needs {
					needsRequeue = true
				}
			} else {
				// For non-terminating services, use standard cleanup
				needs, err := cleanupHandler.CleanupResourcesForService(ctx, svc)
				if err != nil {
					logger.Error(err, "Error cleaning up resources for service",
						"service", svc.Name,
						"namespace", svc.Namespace)
					needsRequeue = true
				} else if needs {
					needsRequeue = true
				}
			}
		}
	}

	// If we need to requeue, don't remove finalizer yet
	if needsRequeue {
		logger.Info("Resource cleanup not complete, requeueing")
		return ctrl.Result{RequeueAfter: RequeueAfter}, nil
	}

	// Remove finalizer and update
	controllerutil.RemoveFinalizer(lb, OpenstackLBFinalizer)
	if err := updateResourceWithRetry(ctx, r.Client, lb); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully removed finalizer")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenStackLoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Set the event recorder
	r.Recorder = mgr.GetEventRecorderFor("openstackloadbalancer-controller")

	// Create a predicate to filter Service events
	servicePredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return false
		}

		// Watch any of these cases:
		// 1. LoadBalancer service type
		// 2. Service has our finalizer (so we can catch deletion events)
		// 3. Service has our annotations (to catch services that are being modified)
		return svc.Spec.Type == corev1.ServiceTypeLoadBalancer ||
			controllerutil.ContainsFinalizer(svc, ServiceFinalizer) ||
			hasAnnotation(svc, AnnotationPortID) ||
			hasAnnotation(svc, AnnotationFloatingIPID)
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&openstackv1alpha1.OpenStackLoadBalancer{}).
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findOpenStackLBsForService),
			builder.WithPredicates(servicePredicate),
		).
		Named("openstackloadbalancer").
		Complete(r)
}
