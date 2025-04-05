package openstack

import (
	"context"
	"fmt"
	"net"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DetectNetworkForIP detects the OpenStack network and subnet for a given IP address
func (c *ClientImpl) DetectNetworkForIP(ctx context.Context, ipAddress string) (string, string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Detecting network for IP", "ipAddress", ipAddress)

	// Parse the IP address
	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return "", "", fmt.Errorf("invalid IP address: %s", ipAddress)
	}

	// List all subnets
	allPages, err := subnets.List(c.networkClient, subnets.ListOpts{}).AllPages()
	if err != nil {
		return "", "", fmt.Errorf("failed to list subnets: %w", err)
	}

	allSubnets, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return "", "", fmt.Errorf("failed to extract subnets: %w", err)
	}

	// Find the subnet that contains the IP address
	for _, subnet := range allSubnets {
		_, cidr, err := net.ParseCIDR(subnet.CIDR)
		if err != nil {
			logger.Error(err, "Failed to parse CIDR", "cidr", subnet.CIDR)
			continue
		}

		if cidr.Contains(ip) {
			logger.Info("Found matching subnet", "subnet", subnet.ID, "network", subnet.NetworkID, "cidr", subnet.CIDR)
			return subnet.NetworkID, subnet.ID, nil
		}
	}

	return "", "", fmt.Errorf("no subnet found containing IP %s", ipAddress)
}
