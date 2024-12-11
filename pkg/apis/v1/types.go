/*
Copyright 2024 The Kubernetes Authors.

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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePool is the Schema for the NodePool API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="NodeClass",type="string",JSONPath=".spec.template.spec.nodeClassRef.name"
// +kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.resources.nodes"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec NodePoolSpec `json:"spec"`

	// +optional
	Status NodePoolStatus `json:"status,omitempty"`
}

// NodePoolList contains a list of NodePool
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

// NodePoolSpec defines the desired state of NodePool
// +k8s:deepcopy-gen=true
type NodePoolSpec struct {
	// +kubebuilder:validation:Required
	Template NodeClaimTemplate `json:"template"`

	// +optional
	Disruption *Disruption `json:"disruption,omitempty"`

	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight *int32 `json:"weight,omitempty"`
}

// NodePoolStatus defines the observed state of NodePool
// +k8s:deepcopy-gen=true
type NodePoolStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Resources corev1.ResourceList `json:"resources,omitempty"`
}

// NodeClaim is the Schema for the NodeClaim API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Class",type="string",JSONPath=".spec.nodeClassRef.name"
// +kubebuilder:printcolumn:name="ProviderID",type="string",JSONPath=".status.providerID"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodeClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec NodeClaimSpec `json:"spec"`

	// +optional
	Status NodeClaimStatus `json:"status,omitempty"`
}

// NodeClaimList contains a list of NodeClaim
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodeClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeClaim `json:"items"`
}

// NodeClaimSpec defines the desired state of NodeClaim
// +k8s:deepcopy-gen=true
type NodeClaimSpec struct {
	// +kubebuilder:validation:Required
	NodeClassRef *NodeClassReference `json:"nodeClassRef"`

	// +kubebuilder:validation:Required
	Requirements []NodeSelectorRequirementWithMinValues `json:"requirements"`

	// +kubebuilder:validation:Required
	Resources ResourceRequirements `json:"resources"`

	// +optional
	Taints []corev1.Taint `json:"taints,omitempty"`
}

// NodeClaimStatus defines the observed state of NodeClaim
// +k8s:deepcopy-gen=true
type NodeClaimStatus struct {
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// +optional
	ImageID string `json:"imageID,omitempty"`

	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`

	// +optional
	Allocatable corev1.ResourceList `json:"allocatable,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NodeClassReference contains information about NodeClass
// +k8s:deepcopy-gen=true
type NodeClassReference struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	APIGroup string `json:"apiGroup"`
}

// NodeClaimTemplate defines the template for creating a NodeClaim
// +k8s:deepcopy-gen=true
type NodeClaimTemplate struct {
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec NodeClaimSpec `json:"spec"`
}

// ResourceRequirements defines the compute resource requirements
// +k8s:deepcopy-gen=true
type ResourceRequirements struct {
	// +kubebuilder:validation:Required
	Requests corev1.ResourceList `json:"requests"`
}

// NodeSelectorRequirementWithMinValues extends NodeSelectorRequirement with MinValues
// +k8s:deepcopy-gen=true
type NodeSelectorRequirementWithMinValues struct {
	corev1.NodeSelectorRequirement `json:",inline"`

	// +optional
	MinValues *int32 `json:"minValues,omitempty"`
}

// Disruption defines the disruption settings
// +k8s:deepcopy-gen=true
type Disruption struct {
	// +optional
	ConsolidateAfter *metav1.Duration `json:"consolidateAfter,omitempty"`

	// +optional
	// +kubebuilder:validation:Enum=WhenEmpty;WhenEmptyOrUnderutilized
	ConsolidationPolicy *string `json:"consolidationPolicy,omitempty"`

	// +optional
	Budgets []Budget `json:"budgets,omitempty"`
}

// Budget defines the disruption budget
// +k8s:deepcopy-gen=true
type Budget struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^((100|[0-9]{1,2})%|[0-9]+)$
	Nodes string `json:"nodes"`
}
