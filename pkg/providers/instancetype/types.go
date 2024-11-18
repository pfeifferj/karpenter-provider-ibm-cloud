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

package instancetype

// InstanceTypeRequirements defines the requirements for an instance type
type InstanceTypeRequirements struct {
	// CPU is the number of CPU cores required
	CPU int
	// Memory is the amount of memory required in GB
	Memory int
	// Architecture is the CPU architecture (e.g., amd64, arm64)
	Architecture string
	// GPU indicates if GPU is required
	GPU bool
}

// InstanceTypeCapabilities defines the capabilities of an instance type
type InstanceTypeCapabilities struct {
	// CPU is the number of CPU cores available
	CPU int
	// Memory is the amount of memory available in GB
	Memory int
	// Architecture is the CPU architecture
	Architecture string
	// GPU indicates if GPU is available
	GPU bool
	// NetworkBandwidth is the network bandwidth in Gbps
	NetworkBandwidth int
	// StorageType is the type of storage available
	StorageType string
}
