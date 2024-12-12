package v1alpha1

import (
	"github.com/awslabs/operatorpkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
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

// PlacementStrategy defines how nodes should be placed across zones and subnets
type PlacementStrategy struct {
	// ZoneBalance determines how nodes are distributed across zones
	// Valid values are:
	// - "Balanced" (default) - Nodes are evenly distributed across zones
	// - "AvailabilityFirst" - Prioritize zone availability over even distribution
	// - "CostOptimized" - Consider both cost and availability when selecting zones
	// +optional
	// +kubebuilder:validation:Enum=Balanced;AvailabilityFirst;CostOptimized
	// +kubebuilder:default=Balanced
	ZoneBalance string `json:"zoneBalance,omitempty"`

	// SubnetSelection defines criteria for automatic subnet selection
	// +optional
	SubnetSelection *SubnetSelectionCriteria `json:"subnetSelection,omitempty"`
}

// SubnetSelectionCriteria defines how subnets should be automatically selected
type SubnetSelectionCriteria struct {
	// MinimumAvailableIPs is the minimum number of available IPs a subnet must have
	// +optional
	// +kubebuilder:validation:Minimum=1
	MinimumAvailableIPs int32 `json:"minimumAvailableIPs,omitempty"`

	// Tags that subnets must have to be considered for selection
	// +optional
	RequiredTags map[string]string `json:"requiredTags,omitempty"`
}

// InstanceTypeRequirements defines criteria for automatic instance type selection
type InstanceTypeRequirements struct {
	// Architecture specifies the CPU architecture
	// Valid values: "amd64", "arm64"
	// +optional
	// +kubebuilder:validation:Enum=amd64;arm64
	Architecture string `json:"architecture,omitempty"`

	// MinimumCPU specifies the minimum number of CPUs required
	// +optional
	// +kubebuilder:validation:Minimum=1
	MinimumCPU int32 `json:"minimumCPU,omitempty"`

	// MinimumMemory specifies the minimum amount of memory in GiB
	// +optional
	// +kubebuilder:validation:Minimum=1
	MinimumMemory int32 `json:"minimumMemory,omitempty"`

	// MaximumHourlyPrice specifies the maximum hourly price in USD as a decimal string (e.g. "0.50")
	// +optional
	// +kubebuilder:validation:Pattern=^\\d+\\.?\\d*$
	MaximumHourlyPrice string `json:"maximumHourlyPrice,omitempty"`
}

// IBMNodeClassSpec defines the desired state of IBMNodeClass
type IBMNodeClassSpec struct {
	// Region is the IBM Cloud region where nodes will be created
	// +required
	Region string `json:"region"`

	// Zone is the availability zone where nodes will be created
	// If not specified, zones will be automatically selected based on placement strategy
	// +optional
	Zone string `json:"zone,omitempty"`

	// InstanceProfile is the name of the instance profile to use
	// If not specified, instance types will be automatically selected based on requirements
	// +optional
	InstanceProfile string `json:"instanceProfile,omitempty"`

	// InstanceRequirements defines requirements for automatic instance type selection
	// Only used when InstanceProfile is not specified
	// +optional
	InstanceRequirements *InstanceTypeRequirements `json:"instanceRequirements,omitempty"`

	// Image is the ID of the image to use for nodes
	// +required
	Image string `json:"image"`

	// VPC is the ID of the VPC where nodes will be created
	// +required
	VPC string `json:"vpc"`

	// Subnet is the ID of the subnet where nodes will be created
	// If not specified, subnets will be automatically selected based on placement strategy
	// +optional
	Subnet string `json:"subnet,omitempty"`

	// PlacementStrategy defines how nodes should be placed across zones and subnets
	// Only used when Zone or Subnet is not specified
	// +optional
	PlacementStrategy *PlacementStrategy `json:"placementStrategy,omitempty"`

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

	// LastValidationTime is the last time the nodeclass was validated
	// +optional
	LastValidationTime metav1.Time `json:"lastValidationTime,omitempty"`

	// ValidationError contains the error message from the last validation
	// +optional
	ValidationError string `json:"validationError,omitempty"`

	// SelectedInstanceTypes contains the list of instance types that meet the requirements
	// Only populated when using automatic instance type selection
	// +optional
	SelectedInstanceTypes []string `json:"selectedInstanceTypes,omitempty"`

	// SelectedSubnets contains the list of subnets selected for node placement
	// Only populated when using automatic subnet selection
	// +optional
	SelectedSubnets []string `json:"selectedSubnets,omitempty"`

	// Conditions contains signals for health and readiness
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StatusConditions returns the condition set for the status.Object interface
func (in *IBMNodeClass) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions().For(in)
}

// GetConditions returns the conditions as status.Conditions for the status.Object interface
func (in *IBMNodeClass) GetConditions() []status.Condition {
	conditions := make([]status.Condition, 0, len(in.Status.Conditions))
	for _, c := range in.Status.Conditions {
		conditions = append(conditions, status.Condition{
			Type:               c.Type,
			Status:             c.Status, // Use c.Status directly as it's already a string-like value
			LastTransitionTime: c.LastTransitionTime,
			Reason:             c.Reason,
			Message:            c.Message,
			ObservedGeneration: c.ObservedGeneration,
		})
	}
	return conditions
}

// SetConditions sets the conditions from status.Conditions for the status.Object interface
func (in *IBMNodeClass) SetConditions(conditions []status.Condition) {
	metav1Conditions := make([]metav1.Condition, 0, len(conditions))
	for _, c := range conditions {
		if c.LastTransitionTime.IsZero() {
			continue
		}
		metav1Conditions = append(metav1Conditions, metav1.Condition{
			Type:               c.Type,
			Status:             metav1.ConditionStatus(c.Status),
			LastTransitionTime: c.LastTransitionTime,
			Reason:             c.Reason,
			Message:            c.Message,
			ObservedGeneration: c.ObservedGeneration,
		})
	}
	in.Status.Conditions = metav1Conditions
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IBMNodeClass{}, &IBMNodeClassList{})
}
