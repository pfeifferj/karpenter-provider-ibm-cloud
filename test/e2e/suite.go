//go:build e2e
// +build e2e

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
package e2e

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/instancetype"
)

// E2ETestSuite contains the test environment
type E2ETestSuite struct {
	kubeClient           client.Client
	ibmClient            *ibm.Client
	cloudProvider        *ibmcloud.CloudProvider
	instanceTypeProvider instancetype.Provider

	// Test IBM Cloud resources
	testVPC    string
	testSubnet string
	testImage  string
	testRegion string
	testZone   string
}
