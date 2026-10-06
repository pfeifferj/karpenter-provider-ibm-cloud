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

package ownership

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	BackendAnnotation   = "karpenter-ibm.sh/provider-mode"
	RegionAnnotation    = "karpenter-ibm.sh/region"
	ClusterIDAnnotation = "karpenter-ibm.sh/cluster-id"
	PoolIDAnnotation    = "karpenter-ibm.sh/worker-pool-id"
	WorkerIDAnnotation  = "karpenter-ibm.sh/worker-id"
	AccountIDAnnotation = "karpenter-ibm.sh/account-id"
	ClusterUIDTag       = "karpenter-ibm.sh/cluster-uid"
	ClaimUIDTag         = "karpenter-ibm.sh/nodeclaim-uid"
	NodeClassUIDTag     = "karpenter-ibm.sh/nodeclass-uid"
	ProviderTag         = "karpenter-ibm.sh/provider"
	ManagedTag          = "karpenter.sh/managed"
)

func ClusterUID(ctx context.Context, reader client.Reader) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("cluster ownership requires a Kubernetes reader")
	}
	namespace := &corev1.Namespace{}
	if err := reader.Get(ctx, types.NamespacedName{Name: "kube-system"}, namespace); err != nil {
		return "", fmt.Errorf("reading cluster identity: %w", err)
	}
	if namespace.UID == "" {
		return "", fmt.Errorf("kube-system namespace has no UID")
	}
	return string(namespace.UID), nil
}

func VPCTags(clusterUID, claimUID, classUID string) map[string]string {
	return map[string]string{
		ManagedTag:      "true",
		ProviderTag:     "vpc",
		ClusterUIDTag:   clusterUID,
		ClaimUIDTag:     claimUID,
		NodeClassUIDTag: classUID,
	}
}

func ReservedTag(key string) bool {
	return strings.HasPrefix(key, "karpenter.sh/") || strings.HasPrefix(key, "karpenter-ibm.sh/")
}

func InstanceName(clusterUID, claimUID string) string {
	identity := sha256.Sum256([]byte(clusterUID + "/" + claimUID))
	return fmt.Sprintf("karpenter-%x", identity[:26])
}
