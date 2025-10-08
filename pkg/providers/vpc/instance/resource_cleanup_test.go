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

package instance

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

func TestIsPartialFailure(t *testing.T) {
	provider := &VPCInstanceProvider{}

	tests := []struct {
		name     string
		ibmErr   *ibm.IBMError
		expected bool
	}{
		{
			name:     "nil error",
			ibmErr:   nil,
			expected: false,
		},
		{
			name: "quota exceeded error",
			ibmErr: &ibm.IBMError{
				StatusCode: 400,
				Code:       "vpc_instance_quota_exceeded",
				Message:    "Instance quota exceeded",
			},
			expected: true,
		},
		{
			name: "resource not found error",
			ibmErr: &ibm.IBMError{
				StatusCode: 404,
				Code:       "not_found",
				Message:    "Instance not found",
			},
			expected: false,
		},
		{
			name: "internal server error",
			ibmErr: &ibm.IBMError{
				StatusCode: 500,
				Code:       "internal_error",
				Message:    "Internal server error",
			},
			expected: true, // 5xx errors are considered partial failures (retryable)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.isPartialFailure(tt.ibmErr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
