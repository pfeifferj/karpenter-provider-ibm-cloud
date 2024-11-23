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

package events

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

func NodeClaimFailedToResolveNodeClass(nodeClaim *karpv1.NodeClaim) events.Event {
	return events.Event{
		Type:    corev1.EventTypeWarning,
		Reason:  "FailedToResolveNodeClass",
		Message: fmt.Sprintf("Failed to resolve NodeClass for NodeClaim %s", nodeClaim.Name),
	}
}

func NodePoolFailedToResolveNodeClass(nodePool *karpv1.NodePool) events.Event {
	return events.Event{
		Type:    corev1.EventTypeWarning,
		Reason:  "FailedToResolveNodeClass",
		Message: fmt.Sprintf("Failed to resolve NodeClass for NodePool %s", nodePool.Name),
	}
}
