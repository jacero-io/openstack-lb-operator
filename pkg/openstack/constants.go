package openstack

// Resource ownership tags/labels
const (
	// TagManagedBy indicates the component that manages the resource
	TagManagedBy = "managed-by"

	// TagManagedByValue is the value for resources managed by this operator
	TagManagedByValue = "openstack-lb-operator"

	// TagServiceNamespace is the namespace of the service associated with the resource
	TagServiceNamespace = "k8s-service-namespace"

	// TagServiceName is the name of the service associated with the resource
	TagServiceName = "k8s-service-name"

	// TagClusterName is the name of the Kubernetes cluster
	TagClusterName = "k8s-cluster-name"

	// TagResourceType is the type of resource this represents
	TagResourceType = "resource-type"

	// ResourceTypeValues
	ResourceTypePort       = "service-port"
	ResourceTypeFloatingIP = "service-floating-ip"
)
