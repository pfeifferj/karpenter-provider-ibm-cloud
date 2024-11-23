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

import "time"

// InstanceStatus represents the current status of an instance
type InstanceStatus string

const (
	// InstanceStatusPending indicates the instance is being created
	InstanceStatusPending InstanceStatus = "pending"
	// InstanceStatusRunning indicates the instance is running
	InstanceStatusRunning InstanceStatus = "running"
	// InstanceStatusStopping indicates the instance is being stopped
	InstanceStatusStopping InstanceStatus = "stopping"
	// InstanceStatusStopped indicates the instance has been stopped
	InstanceStatusStopped InstanceStatus = "stopped"
)

// Instance represents an IBM Cloud instance
type Instance struct {
	// ID is the instance ID
	ID string
	// Name is the instance name
	Name string
	// Type is the instance type
	Type string
	// Region is the region where the instance is located
	Region string
	// Zone is the availability zone where the instance is located
	Zone string
	// Status is the current status of the instance
	Status InstanceStatus
	// State represents the current state of the instance
	State string
	// ImageID is the ID of the image used to create the instance
	ImageID string
	// CreationTime is when the instance was created
	CreationTime time.Time
	// LaunchTime is when the instance was created (string format)
	LaunchTime string
	// CapacityType represents the type of capacity (on-demand, spot, etc)
	CapacityType string
	// Tags are key/value pairs attached to the instance
	Tags map[string]string
}

// InstanceOptions contains configuration for instance creation
type InstanceOptions struct {
	// Type is the instance type to create
	Type string
	// Zone is the availability zone to create the instance in
	Zone string
	// Labels are the Kubernetes labels to apply to the instance
	Labels map[string]string
	// Taints are the Kubernetes taints to apply to the instance
	Taints []string
}
