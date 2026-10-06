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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestCloudOwnershipTagsPreserveKubernetesLabelContract(t *testing.T) {
	for _, contract := range []struct {
		label, tag string
	}{
		{ClusterUIDLabel, ClusterUIDTag},
		{ClaimUIDLabel, ClaimUIDTag},
		{NodeClassUIDLabel, NodeClassUIDTag},
		{ProviderLabel, ProviderTag},
		{ManagedLabel, ManagedTag},
	} {
		require.Empty(t, validation.IsQualifiedName(contract.label))
		require.NotEqual(t, contract.label, contract.tag)
		require.NotContains(t, contract.tag, "/")
		require.True(t, ReservedTag(contract.label))
		require.True(t, ReservedTag(contract.tag))
	}
	formatted, err := FormatTags(VPCTags("cluster-uid", "claim-uid", "class-uid"))
	require.NoError(t, err)
	require.Len(t, formatted, 5)
	for _, tag := range formatted {
		require.NoError(t, ValidateTag(tag))
		key, _, _ := strings.Cut(tag, ":")
		require.True(t, ReservedTag(key), tag)
	}
	require.NoError(t, ValidateTag(RetainTag+":true"))
}

func TestReservedTagIncludesBothNamespacesWithoutCaseBypass(t *testing.T) {
	for _, key := range []string{
		"karpenter.sh/managed", "karpenter-ibm.sh/cluster-uid", "karpenter.sh.managed", "karpenter-ibm.sh.cluster-uid",
		"KARPENTER-IBM.SH.CLUSTER-UID", "Karpenter.sh/managed", RetainTag, "managed-by", "Managed-By",
	} {
		require.True(t, ReservedTag(key), key)
	}
	for _, key := range []string{"test", "purpose", "karpenter-ibm.shx.owner", "example.com/karpenter-ibm.sh/cluster-uid"} {
		require.False(t, ReservedTag(key), key)
	}
}

func TestFormatTagsValidatesCombinedLengthAndUnambiguousKeys(t *testing.T) {
	formatted, err := FormatTags(map[string]string{"z-key": "A_z-0. :value", "a-key": "value"})
	require.NoError(t, err)
	require.Equal(t, []string{"a-key:value", "z-key:A_z-0. :value"}, formatted)

	formatted, err = FormatTags(map[string]string{"k": strings.Repeat("v", 126)})
	require.NoError(t, err)
	require.Len(t, formatted[0], 128)
	for _, tags := range []map[string]string{
		{"k": strings.Repeat("v", 127)}, {strings.Repeat("k", 128): ""}, {"": "value"}, {"key:part": "value"},
		{"key/name": "value"}, {"key": "comma,value"}, {"key": "unicode-\u00e9"}, {"key": "line\nbreak"},
	} {
		formatted, err = FormatTags(tags)
		require.Error(t, err)
		require.Nil(t, formatted)
	}
}

func TestValidateRawCloudTags(t *testing.T) {
	for _, tag := range []string{"plain-tag", "env:dev", "schedule:24:7", "ASCII 0_9-A.z", strings.Repeat("a", 128)} {
		require.NoError(t, ValidateTag(tag))
	}
	for _, tag := range []string{"", strings.Repeat("a", 129), "karpenter.sh/managed:true", "a,b", "a@b", "a\tb", "a\u00e9b"} {
		require.Error(t, ValidateTag(tag))
	}
}
