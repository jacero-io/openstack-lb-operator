/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OpenStackLoadBalancerSpec defines the desired state of OpenStackLoadBalancer.
type OpenStackLoadBalancerSpec struct {
	// ClassName for the loadbalancer class
	// +kubebuilder:validation:Required
	ClassName string `json:"className"`

	// ApplicationCredentialSecretRef is the reference to a secret with OpenStack application credentials
	// +kubebuilder:validation:Required
	ApplicationCredentialSecretRef SecretReference `json:"applicationCredentialSecretRef"`

	// CreateFloatingIP determines whether to create a floating IP for the load balancer
	// +kubebuilder:default=false
	CreateFloatingIP bool `json:"createFloatingIP,omitempty"`

	// FloatingIPNetworkID is the ID of the external network for floating IP allocation
	// Only required if createFloatingIP is true
	// +optional
	FloatingIPNetworkID string `json:"floatingIPNetworkID,omitempty"`
}

// SecretReference contains information about the secret containing OpenStack credentials
type SecretReference struct {
	// Name is the name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the secret
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// OpenStackLoadBalancerStatus defines the observed state of OpenStackLoadBalancer.
type OpenStackLoadBalancerStatus struct {
	// Conditions represents the latest available observations of the load balancer's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DetectedNetworkID is the auto-detected OpenStack network ID
	// +optional
	LBNetworkID string `json:"loadbalancerNetworkID,omitempty"`

	// DetectedSubnetID is the auto-detected OpenStack subnet ID
	// +optional
	LBSubnetID string `json:"loadbalancerSubnetID,omitempty"`

	// Ready indicates if the OpenStack resources have been successfully created and configured
	// +optional
	Ready bool `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Class",type="string",JSONPath=".spec.className",description="Load Balancer Class"
// +kubebuilder:printcolumn:name="LBNetworkID",type="string",JSONPath=".status.loadbalancerNetworkID",description="Loadbalancer Network ID"
// +kubebuilder:printcolumn:name="LBSubnetID",type="string",JSONPath=".status.loadbalancerSubnetID",description="Loadbalancer Subnet ID"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=oslb

// OpenStackLoadBalancer is the Schema for the openstackloadbalancers API.
type OpenStackLoadBalancer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenStackLoadBalancerSpec   `json:"spec,omitempty"`
	Status OpenStackLoadBalancerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpenStackLoadBalancerList contains a list of OpenStackLoadBalancer.
type OpenStackLoadBalancerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenStackLoadBalancer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenStackLoadBalancer{}, &OpenStackLoadBalancerList{})
}
