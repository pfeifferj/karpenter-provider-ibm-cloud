package v1alpha1

import (
	"github.com/awslabs/operatorpkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
type IBMNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of IBMNodeClass
	Spec IBMNodeClassSpec `json:"spec,omitempty"`

	// Status defines the observed state of IBMNodeClass
	Status IBMNodeClassStatus `json:"status,omitempty"`
}

// IBMNodeClassSpec defines the desired state of IBMNodeClass
type IBMNodeClassSpec struct {
	// Region is the IBM Cloud region where nodes will be created
	// +required
	Region string `json:"region"`

	// Zone is the availability zone where nodes will be created
	// If not specified, the zone will be automatically selected
	// +optional
	Zone string `json:"zone,omitempty"`

	// InstanceProfile is the name of the instance profile to use
	// +required
	InstanceProfile string `json:"instanceProfile"`

	// Image is the ID of the image to use for nodes
	// +required
	Image string `json:"image"`

	// VPC is the ID of the VPC where nodes will be created
	// +required
	VPC string `json:"vpc"`

	// Subnet is the ID of the subnet where nodes will be created
	// +required
	Subnet string `json:"subnet"`

	// SecurityGroups is a list of security group IDs to attach to nodes
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Tags to apply to the instances
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// IBMNodeClassStatus defines the observed state of IBMNodeClass
type IBMNodeClassStatus struct {
	// SpecHash is a hash of the IBMNodeClass spec
	// +optional
	SpecHash uint64 `json:"specHash,omitempty"`

	// Conditions contains signals for health and readiness
	// +optional
	Conditions []status.Condition `json:"conditions,omitempty"`
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IBMNodeClass{}, &IBMNodeClassList{})
}
