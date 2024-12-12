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
	"github.com/awslabs/operatorpkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatusConditions returns the condition set for the status.Object interface
func (in *IBMNodeClass) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions().For(in)
}

// GetConditions returns the conditions as status.Conditions for the status.Object interface
func (in *IBMNodeClass) GetConditions() []status.Condition {
	conditions := make([]status.Condition, 0, len(in.Status.Conditions))
	for _, c := range in.Status.Conditions {
		lastTransitionTime := c.LastTransitionTime
		conditions = append(conditions, status.Condition{
			Type:               c.Type,
			Status:             string(c.Status),
			LastTransitionTime: &lastTransitionTime,
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
		if c.LastTransitionTime == nil {
			continue
		}
		metav1Conditions = append(metav1Conditions, metav1.Condition{
			Type:               c.Type,
			Status:             metav1.ConditionStatus(c.Status),
			LastTransitionTime: *c.LastTransitionTime,
			Reason:             c.Reason,
			Message:            c.Message,
			ObservedGeneration: c.ObservedGeneration,
		})
	}
	in.Status.Conditions = metav1Conditions
}
