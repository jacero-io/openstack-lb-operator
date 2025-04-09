package openstack

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

	// Create a transport with custom CA certificates if configured
	transport, err := createHTTPTransport(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP transport: %w", err)
	}

	// Configure provider options with our custom transport
	providerClient, err := openstack.NewClient(authOpts.IdentityEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenStack provider client: %w", err)
	}

	// Set the custom transport
	if transport != nil {
		providerClient.HTTPClient.Transport = transport
	}

	// Authenticate with OpenStack
	err = openstack.Authenticate(providerClient, authOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with OpenStack: %w", err)
	}

	// Create networking client
	networkClient, err := openstack.NewNetworkV2(providerClient, gophercloud.EndpointOpts{
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenStack network client: %w", err)
	}

	return &ClientImpl{
		networkClient: networkClient,
	}, nil
}

// createHTTPTransport creates an HTTP transport with custom CA certificates if available
func createHTTPTransport(ctx context.Context) (*http.Transport, error) {
	logger := log.FromContext(ctx)
	
	// Check if custom CA certificates path is defined
	caPath := os.Getenv("OPENSTACK_CA_CERT_PATH")
	if caPath == "" {
		logger.V(1).Info("No custom CA certificate path specified, using system certificates")
		return nil, nil
	}

	logger.Info("Loading custom CA certificates", "path", caPath)
	
	// Load system certificates
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		logger.Info("Could not load system certificate pool, creating new pool", "error", err.Error())
		rootCAs = x509.NewCertPool()
	}

	// Check if the CA path is a directory or a file
	fileInfo, err := os.Stat(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access CA path %s: %w", caPath, err)
	}

	// Counter for loaded certificates
	certCount := 0

	if fileInfo.IsDir() {
		// It's a directory, load all certificate files in it
		err = filepath.WalkDir(caPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Filter for typical certificate extensions and names
			lowerName := strings.ToLower(d.Name())
			if strings.HasSuffix(lowerName, ".crt") || 
			   strings.HasSuffix(lowerName, ".pem") || 
			   strings.HasSuffix(lowerName, ".cert") ||
			   lowerName == "ca-certificates" ||
			   lowerName == "ca-bundle" {
				
				certs, err := os.ReadFile(path)
				if err != nil {
					logger.Error(err, "Failed to read certificate file", "path", path)
					return nil // Continue with other certificates
				}

				if ok := rootCAs.AppendCertsFromPEM(certs); ok {
					certCount++
					logger.V(1).Info("Added certificates from file", "path", path)
				} else {
					logger.Info("No certificates could be parsed from file", "path", path)
				}
			}
			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("error walking CA certificate directory: %w", err)
		}
	} else {
		// It's a file, load it directly
		certs, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file %s: %w", caPath, err)
		}

		if ok := rootCAs.AppendCertsFromPEM(certs); ok {
			certCount++
			logger.V(1).Info("Added certificates from file", "path", caPath)
		} else {
			logger.Info("No certificates could be parsed from file", "path", caPath)
		}
	}

	// Create a custom transport with our certificate pool
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: rootCAs,
		},
	}

	logger.Info("CA certificates loaded successfully", "count", certCount)
	return transport, nil
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