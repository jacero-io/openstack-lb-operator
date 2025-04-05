package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientImpl implements the Client interface
type ClientImpl struct {
	networkClient *gophercloud.ServiceClient
}

// NewClient creates a new OpenStack client from K8s secret
func NewClient(ctx context.Context, k8sClient client.Client, secretName, secretNamespace string) (Client, error) {
	// Get the OpenStack credentials secret
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: secretNamespace}, secret); err != nil {
		return nil, fmt.Errorf("failed to get OpenStack credentials secret: %w", err)
	}

	// Extract credentials from the secret
	authURL := string(secret.Data["OS_AUTH_URL"])
	region := string(secret.Data["OS_REGION_NAME"])
	appID := string(secret.Data["OS_APPLICATION_CREDENTIAL_ID"])
	appSecret := string(secret.Data["OS_APPLICATION_CREDENTIAL_SECRET"])

	if authURL == "" || appID == "" || appSecret == "" {
		return nil, fmt.Errorf("missing required OpenStack credentials in secret")
	}

	// Create OpenStack authentication options
	authOpts := gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		ApplicationCredentialID:     appID,
		ApplicationCredentialSecret: appSecret,
	}

	// Authenticate with OpenStack
	provider, err := openstack.AuthenticatedClient(authOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with OpenStack: %w", err)
	}

	// Create networking client
	networkClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenStack network client: %w", err)
	}

	return &ClientImpl{
		networkClient: networkClient,
	}, nil
}

// IsNotFoundError checks if an error is a "not found" error
func IsNotFoundError(err error) bool {
	if _, ok := err.(gophercloud.ErrDefault404); ok {
		return true
	}
	return false
}

// Ensure ClientImpl implements Client interface
var _ Client = &ClientImpl{}
