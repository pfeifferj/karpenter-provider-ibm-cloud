/*
Copyright The Kubernetes Authors.

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

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ibmnodeclasses,scope=Cluster,categories=karpenter,shortName={ibmnc,ibmncs}
// +kubebuilder:subresource:status
type IBMNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:default:={conditions: {{type: "Ready", status: "True", reason:"Ready", lastTransitionTime: "2024-01-01T01:01:01Z", message: ""}}}
	Status IBMNodeClassStatus `json:"status,omitempty"`
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}

const DisruptionReasonExampleReason v1.DisruptionReason = "ExampleReason"
