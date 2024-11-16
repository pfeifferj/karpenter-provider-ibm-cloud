package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IBMNodeClassSpec defines the desired state of IBMNodeClass
type IBMNodeClassSpec struct {
	// Region is the IBM Cloud region where nodes will be provisioned
	// +required
	Region string `json:"region"`

	// Zones is the list of zones within the region for node placement
	// +optional
	Zones []string `json:"zones,omitempty"`

	// VPC is the VPC ID where nodes will be created
	// +optional
	VPC string `json:"vpc,omitempty"`

	// ResourceGroup is the resource group for the provisioned resources
	// +optional
	ResourceGroup string `json:"resourceGroup,omitempty"`

	// SecurityGroups are the security group IDs to attach to nodes
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Tags to apply to provisioned resources
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// IBMNodeClassStatus defines the observed state of IBMNodeClass
type IBMNodeClassStatus struct {
	// Conditions contains signals for health and readiness
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type IBMNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IBMNodeClassSpec   `json:"spec,omitempty"`
	Status IBMNodeClassStatus `json:"status,omitempty"`
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}
