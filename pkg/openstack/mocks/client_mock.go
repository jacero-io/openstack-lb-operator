package mocks

import (
	"context"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/jacero-io/openstack-lb-operator/pkg/openstack"
)

// MockClient is a mock implementation of the OpenStack client for testing
type MockClient struct {
	DetectNetworkForIPFunc func(ctx context.Context, ipAddress string) (string, string, error)

	CreatePortFunc func(
		ctx context.Context,
		networkID, subnetID, portName, ipAddress, description string,
		serviceNamespace, serviceName string,
	) (string, error)

	GetPortFunc         func(ctx context.Context, portID string) (bool, error)
	DeletePortFunc      func(ctx context.Context, portID string) error
	GetManagedPortsFunc func(ctx context.Context, serviceNamespace, serviceName string) ([]ports.Port, error)

	CreateFloatingIPFunc func(
		ctx context.Context,
		networkID, portID, description string,
		serviceNamespace, serviceName string,
	) (string, string, error)

	GetFloatingIPFunc         func(ctx context.Context, floatingIPID string) (bool, error)
	DeleteFloatingIPFunc      func(ctx context.Context, floatingIPID string) error
	GetFloatingIPByPortIDFunc func(ctx context.Context, portID string) (string, string, error)

	GetManagedFloatingIPsFunc func(
		ctx context.Context,
		serviceNamespace, serviceName string,
	) ([]floatingips.FloatingIP, error)

	GetAllFloatingIPsFunc func(ctx context.Context) ([]floatingips.FloatingIP, error)
	GetAllPortsFunc       func(ctx context.Context) ([]ports.Port, error)
	GetFloatingIPDetailsFunc func(ctx context.Context, floatingIPID string) (*floatingips.FloatingIP, error)
}

// Ensure MockClient implements Client interface
var _ openstack.Client = &MockClient{}

// DetectNetworkForIP implements the corresponding interface method
func (m *MockClient) DetectNetworkForIP(ctx context.Context, ipAddress string) (string, string, error) {
	if m.DetectNetworkForIPFunc != nil {
		return m.DetectNetworkForIPFunc(ctx, ipAddress)
	}
	return "mock-network-id", "mock-subnet-id", nil
}

// CreatePort implements the corresponding interface method
func (m *MockClient) CreatePort(
	ctx context.Context,
	networkID, subnetID, portName, ipAddress, description string,
	serviceNamespace, serviceName string,
) (string, error) {
	if m.CreatePortFunc != nil {
		return m.CreatePortFunc(ctx, networkID, subnetID, portName, ipAddress, description, serviceNamespace, serviceName)
	}
	return "mock-port-id", nil
}

// GetPort implements the corresponding interface method
func (m *MockClient) GetPort(ctx context.Context, portID string) (bool, error) {
	if m.GetPortFunc != nil {
		return m.GetPortFunc(ctx, portID)
	}
	return true, nil
}

// DeletePort implements the corresponding interface method
func (m *MockClient) DeletePort(ctx context.Context, portID string) error {
	if m.DeletePortFunc != nil {
		return m.DeletePortFunc(ctx, portID)
	}
	return nil
}

// GetManagedPorts implements the corresponding interface method
func (m *MockClient) GetManagedPorts(ctx context.Context, serviceNamespace, serviceName string) ([]ports.Port, error) {
	if m.GetManagedPortsFunc != nil {
		return m.GetManagedPortsFunc(ctx, serviceNamespace, serviceName)
	}
	return []ports.Port{}, nil
}

// CreateFloatingIP implements the corresponding interface method
func (m *MockClient) CreateFloatingIP(
	ctx context.Context,
	networkID, portID, description string,
	serviceNamespace, serviceName string,
) (string, string, error) {
	if m.CreateFloatingIPFunc != nil {
		return m.CreateFloatingIPFunc(ctx, networkID, portID, description, serviceNamespace, serviceName)
	}
	return "mock-floating-ip-id", "192.168.1.100", nil
}

// GetFloatingIP implements the corresponding interface method
func (m *MockClient) GetFloatingIP(ctx context.Context, floatingIPID string) (bool, error) {
	if m.GetFloatingIPFunc != nil {
		return m.GetFloatingIPFunc(ctx, floatingIPID)
	}
	return true, nil
}

// GetFloatingIPByPortID implements the corresponding interface method
func (m *MockClient) GetFloatingIPByPortID(ctx context.Context, portID string) (string, string, error) {
	if m.GetFloatingIPByPortIDFunc != nil {
		return m.GetFloatingIPByPortIDFunc(ctx, portID)
	}
	return "", "", nil
}

// DeleteFloatingIP implements the corresponding interface method
func (m *MockClient) DeleteFloatingIP(ctx context.Context, floatingIPID string) error {
	if m.DeleteFloatingIPFunc != nil {
		return m.DeleteFloatingIPFunc(ctx, floatingIPID)
	}
	return nil
}

// GetManagedFloatingIPs implements the corresponding interface method
func (m *MockClient) GetManagedFloatingIPs(
	ctx context.Context,
	serviceNamespace, serviceName string,
) ([]floatingips.FloatingIP, error) {
	if m.GetManagedFloatingIPsFunc != nil {
		return m.GetManagedFloatingIPsFunc(ctx, serviceNamespace, serviceName)
	}
	return []floatingips.FloatingIP{}, nil
}

// GetAllFloatingIPs implements the corresponding interface method
func (m *MockClient) GetAllFloatingIPs(ctx context.Context) ([]floatingips.FloatingIP, error) {
	if m.GetAllFloatingIPsFunc != nil {
		return m.GetAllFloatingIPsFunc(ctx)
	}
	return []floatingips.FloatingIP{}, nil
}

// GetAllPorts implements the corresponding interface method
func (m *MockClient) GetAllPorts(ctx context.Context) ([]ports.Port, error) {
	if m.GetAllPortsFunc != nil {
		return m.GetAllPortsFunc(ctx)
	}
	return []ports.Port{}, nil
}

func (m *MockClient) GetFloatingIPDetails(ctx context.Context, floatingIPID string) (*floatingips.FloatingIP, error) {
    if m.GetFloatingIPDetailsFunc != nil {
        return m.GetFloatingIPDetailsFunc(ctx, floatingIPID)
    }
    return &floatingips.FloatingIP{
        ID:         floatingIPID,
        FloatingIP: "192.168.1.100",
    }, nil
}
