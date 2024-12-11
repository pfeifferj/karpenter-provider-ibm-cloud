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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/interfaces"
)

var _ interfaces.NodeClass = &IBMNodeClass{}

// IBMNodeClassSpec defines the desired state of IBMNodeClass
// +k8s:deepcopy-gen=true
type IBMNodeClassSpec struct {
	// Region is the IBM Cloud region where nodes will be provisioned
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// Zones is the list of zones within the region for node placement
	// +optional
	Zones []string `json:"zones,omitempty"`

	// VPC is the VPC ID where nodes will be created
	// +kubebuilder:validation:Required
	VPC string `json:"vpc"`

	// Subnet is the subnet ID where nodes will be created
	// +kubebuilder:validation:Required
	Subnet string `json:"subnet"`

	// InstanceProfile is the instance profile to use for nodes
	// +kubebuilder:validation:Required
	InstanceProfile string `json:"instanceProfile"`

	// Image is the ID of the image to use for nodes
	// +kubebuilder:validation:Required
	Image string `json:"image"`

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

// IBMNodeClassStatus contains the resolved state of the IBMNodeClass
// +k8s:deepcopy-gen=true
type IBMNodeClassStatus struct {
	// Conditions contains signals for health and readiness
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// SpecHash is a hash of the IBMNodeClass spec
	// +optional
	SpecHash int64 `json:"specHash,omitempty"`

	// LastValidationTime is the last time the nodeclass was validated
	// +optional
	LastValidationTime metav1.Time `json:"lastValidationTime,omitempty"`

	// ValidationError contains any validation error message
	// +optional
	ValidationError string `json:"validationError,omitempty"`
}

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={karpenter},shortNames={ibmnc;ibmncs}
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion
// +k8s:deepcopy-gen=true
type IBMNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec IBMNodeClassSpec `json:"spec"`

	// +optional
	Status IBMNodeClassStatus `json:"status,omitempty"`
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}

// StatusConditions returns the status conditions of the object
func (in *IBMNodeClass) StatusConditions() *[]metav1.Condition {
	return &in.Status.Conditions
}

func (in *IBMNodeClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *IBMNodeClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
