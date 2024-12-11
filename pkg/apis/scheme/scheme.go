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

package scheme

import (
	"k8s.io/apimachinery/pkg/runtime"

	ibmv1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1"
	ibmv1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// AddToScheme adds all types of this clientset into the given scheme.
func AddToScheme(s *runtime.Scheme) error {
	// Add v1alpha1 first since v1 might depend on it
	if err := ibmv1alpha1.AddToScheme(s); err != nil {
		return err
	}
	if err := ibmv1.AddToScheme(s); err != nil {
		return err
	}
	return nil
}
