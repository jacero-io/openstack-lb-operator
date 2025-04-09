package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Client defines the OpenStack client interface
type Client interface {
	// Combines all the other client interfaces
	NetworkClient
	PortClient
	FloatingIPClient
}

// NetworkClient handles network operations
type NetworkClient interface {
	// DetectNetworkForIP finds the network and subnet for a given IP
	DetectNetworkForIP(ctx context.Context, ipAddress string) (networkID, subnetID string, err error)

	// GetAllFloatingIPs gets all floating IPs in OpenStack
	GetAllFloatingIPs(ctx context.Context) ([]floatingips.FloatingIP, error)

	// GetAllPorts gets all ports in OpenStack
	GetAllPorts(ctx context.Context) ([]ports.Port, error)

	// GetFloatingIPDetails gets details of a specific floating IP by ID
	GetFloatingIPDetails(ctx context.Context, floatingIPID string) (*floatingips.FloatingIP, error)
}

// PortClient handles port operations
type PortClient interface {
	// CreatePort creates an OpenStack port
	CreatePort(ctx context.Context, networkID, subnetID, portName, ipAddress, description string,
		serviceNamespace, serviceName string) (portID string, err error)

	// GetPort checks if a port exists
	GetPort(ctx context.Context, portID string) (exists bool, err error)

	// DeletePort deletes an OpenStack port
	DeletePort(ctx context.Context, portID string) error

	// GetManagedPorts gets all ports managed by this operator for a specific service
	GetManagedPorts(ctx context.Context, serviceNamespace, serviceName string) ([]ports.Port, error)
}

// FloatingIPClient handles floating IP operations
type FloatingIPClient interface {
	// CreateFloatingIP creates a floating IP and associates it with a port
	CreateFloatingIP(ctx context.Context, networkID, portID, description string,
		serviceNamespace, serviceName string) (floatingIPID, floatingIPAddress string, err error)

	// GetFloatingIP checks if a floating IP exists
	GetFloatingIP(ctx context.Context, floatingIPID string) (exists bool, err error)

	// DeleteFloatingIP deletes a floating IP
	DeleteFloatingIP(ctx context.Context, floatingIPID string) error

	// GetFloatingIPByPortID finds a floating IP associated with a port
	GetFloatingIPByPortID(ctx context.Context, portID string) (floatingIPID, floatingIPAddress string, err error)

	// GetManagedFloatingIPs gets all floating IPs managed by this operator for a specific service
	GetManagedFloatingIPs(ctx context.Context, serviceNamespace, serviceName string) ([]floatingips.FloatingIP, error)
}

// In pkg/openstack/floatingip.go
func (c *ClientImpl) GetAllFloatingIPs(ctx context.Context) ([]floatingips.FloatingIP, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing all floating IPs in OpenStack")

	// List all floating IPs
	allPages, err := floatingips.List(c.networkClient, floatingips.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to list floating IPs: %w", err)
	}

	allFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract floating IPs: %w", err)
	}

	return allFIPs, nil
}

// In pkg/openstack/port.go
func (c *ClientImpl) GetAllPorts(ctx context.Context) ([]ports.Port, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Listing all ports in OpenStack")

	// List all ports
	allPages, err := ports.List(c.networkClient, ports.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to list ports: %w", err)
	}

	allPorts, err := ports.ExtractPorts(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ports: %w", err)
	}

	return allPorts, nil
}
