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

package workerpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsInstanceTypeAllowed(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		allowedTypes []string
		expected     bool
	}{
		{
			name:         "empty list allows all",
			instanceType: "bx2-4x16",
			allowedTypes: []string{},
			expected:     true,
		},
		{
			name:         "nil list allows all",
			instanceType: "bx2-4x16",
			allowedTypes: nil,
			expected:     true,
		},
		{
			name:         "type in allowed list",
			instanceType: "bx2-4x16",
			allowedTypes: []string{"bx2-2x8", "bx2-4x16", "bx2-8x32"},
			expected:     true,
		},
		{
			name:         "type not in allowed list",
			instanceType: "cx2-4x8",
			allowedTypes: []string{"bx2-2x8", "bx2-4x16", "bx2-8x32"},
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInstanceTypeAllowed(tt.instanceType, tt.allowedTypes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
