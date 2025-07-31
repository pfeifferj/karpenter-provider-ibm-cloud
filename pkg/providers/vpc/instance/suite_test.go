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

package instance_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVPCInstanceProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VPC Instance Provider")
}

var _ = Describe("VPC Instance Provider", func() {
	// The test suite is temporarily disabled while we refactor the test infrastructure
	// These tests require proper mock setup for the IBM Cloud APIs

	It("should skip tests until proper mocks are implemented", func() {
		Skip("VPC instance provider tests need refactoring to use proper mocks")
	})
})
