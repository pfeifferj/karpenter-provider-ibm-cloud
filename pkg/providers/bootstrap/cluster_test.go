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

package bootstrap

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDiscoverClusterConfig(t *testing.T) {
	tests := []struct {
		name      string
		objects   []runtime.Object
		setupMock func(*fake.Clientset)
		want      *ClusterConfig
		wantErr   bool
	}{
		{
			name: "successful discovery with kube-dns IPv4",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-dns",
						Namespace: "kube-system",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.10",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "10.244.0.0/16",
					},
				},
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "calico-node",
						Namespace: "kube-system",
					},
				},
			},
			want: &ClusterConfig{
				DNSClusterIP: "10.96.0.10",
				ClusterCIDR:  "10.244.0.0/16",
				CNIPlugin:    "calico",
				IPFamily:     "ipv4",
			},
		},
		{
			name: "successful discovery with CoreDNS IPv6",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "coredns",
						Namespace: "kube-system",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "fd00::10",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "fd00::/64",
					},
				},
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cilium",
						Namespace: "kube-system",
					},
				},
			},
			want: &ClusterConfig{
				DNSClusterIP: "fd00::10",
				ClusterCIDR:  "fd00::/64",
				CNIPlugin:    "cilium",
				IPFamily:     "ipv6",
			},
		},
		{
			name: "discovery with label selector DNS",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dns-service",
						Namespace: "kube-system",
						Labels:    map[string]string{"k8s-app": "kube-dns"},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.20",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "10.244.0.0/16",
					},
				},
			},
			want: &ClusterConfig{
				DNSClusterIP: "10.96.0.20",
				ClusterCIDR:  "10.244.0.0/16",
				CNIPlugin:    "unknown",
				IPFamily:     "ipv4",
			},
		},
		{
			name:    "no DNS service found",
			objects: []runtime.Object{},
			wantErr: true,
		},
		{
			name: "no nodes found",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-dns",
						Namespace: "kube-system",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.10",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "DNS discovery error",
			objects: []runtime.Object{},
			setupMock: func(c *fake.Clientset) {
				c.PrependReactor("get", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("DNS service error")
				})
				c.PrependReactor("list", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("DNS list error")
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := DiscoverClusterConfig(context.Background(), client)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverClusterConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != nil {
				if got.DNSClusterIP != tt.want.DNSClusterIP {
					t.Errorf("DNSClusterIP = %v, want %v", got.DNSClusterIP, tt.want.DNSClusterIP)
				}
				if got.ClusterCIDR != tt.want.ClusterCIDR {
					t.Errorf("ClusterCIDR = %v, want %v", got.ClusterCIDR, tt.want.ClusterCIDR)
				}
				if got.CNIPlugin != tt.want.CNIPlugin {
					t.Errorf("CNIPlugin = %v, want %v", got.CNIPlugin, tt.want.CNIPlugin)
				}
				if got.IPFamily != tt.want.IPFamily {
					t.Errorf("IPFamily = %v, want %v", got.IPFamily, tt.want.IPFamily)
				}
			}
		})
	}
}

func TestDiscoverDNSClusterIP(t *testing.T) {
	tests := []struct {
		name      string
		objects   []runtime.Object
		setupMock func(*fake.Clientset)
		want      string
		wantErr   bool
	}{
		{
			name: "kube-dns found",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-dns",
						Namespace: "kube-system",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.10",
					},
				},
			},
			want: "10.96.0.10",
		},
		{
			name: "CoreDNS found",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "coredns",
						Namespace: "kube-system",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.20",
					},
				},
			},
			want: "10.96.0.20",
		},
		{
			name: "DNS with label selector",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "custom-dns",
						Namespace: "kube-system",
						Labels:    map[string]string{"k8s-app": "kube-dns"},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.30",
					},
				},
			},
			want: "10.96.0.30",
		},
		{
			name:    "no DNS service",
			objects: []runtime.Object{},
			wantErr: true,
		},
		{
			name: "list services error",
			setupMock: func(c *fake.Clientset) {
				c.PrependReactor("get", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("not found")
				})
				c.PrependReactor("list", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("list error")
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := discoverDNSClusterIP(context.Background(), client)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverDNSClusterIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("discoverDNSClusterIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverClusterCIDR(t *testing.T) {
	tests := []struct {
		name      string
		objects   []runtime.Object
		setupMock func(*fake.Clientset)
		want      string
		wantErr   bool
	}{
		{
			name: "node with PodCIDR",
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "10.244.0.0/16",
					},
				},
			},
			want: "10.244.0.0/16",
		},
		{
			name: "node without PodCIDR falls back to service CIDR",
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						PodCIDR: "",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.1",
					},
				},
			},
			want: "10.96.0.0/12",
		},
		{
			name:    "no nodes found",
			objects: []runtime.Object{},
			wantErr: true,
		},
		{
			name: "node list error",
			setupMock: func(c *fake.Clientset) {
				c.PrependReactor("list", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("list nodes error")
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := discoverClusterCIDR(context.Background(), client)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverClusterCIDR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("discoverClusterCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverServiceCIDR(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
		want    string
		wantErr bool
	}{
		{
			name: "standard 10.96.x.x service IP",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.0.1",
					},
				},
			},
			want: "10.96.0.0/12",
		},
		{
			name: "172.20.x.x service IP",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.20.0.1",
					},
				},
			},
			want: "172.20.0.0/16",
		},
		{
			name: "non-standard IPv4",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "192.168.0.1",
					},
				},
			},
			want: "10.96.0.0/12",
		},
		{
			name: "IPv6 service IP",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "fd00::1",
					},
				},
			},
			want: "fd00::/108",
		},
		{
			name: "invalid service IP",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "invalid-ip",
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "kubernetes service not found",
			objects: []runtime.Object{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)

			got, err := discoverServiceCIDR(context.Background(), client)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverServiceCIDR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("discoverServiceCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectCNIPlugin(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
		want    string
	}{
		{
			name: "calico detected",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "calico-node",
						Namespace: "kube-system",
					},
				},
			},
			want: "calico",
		},
		{
			name: "cilium detected",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cilium",
						Namespace: "kube-system",
					},
				},
			},
			want: "cilium",
		},
		{
			name: "flannel detected",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-flannel-ds",
						Namespace: "kube-system",
					},
				},
			},
			want: "flannel",
		},
		{
			name: "weave detected",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "weave-net",
						Namespace: "kube-system",
					},
				},
			},
			want: "weave",
		},
		{
			name: "aws-vpc-cni not detected (IBM Cloud doesn't use AWS VPC CNI)",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "aws-node",
						Namespace: "kube-system",
					},
				},
			},
			want: "unknown",
		},
		{
			name: "cni config map with calico",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cni-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"config.conf": "calico configuration here",
					},
				},
			},
			want: "calico",
		},
		{
			name: "kube-proxy config with cilium",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-proxy",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"config.conf": "some cilium settings",
					},
				},
			},
			want: "cilium",
		},
		{
			name:    "no CNI detected",
			objects: []runtime.Object{},
			want:    "unknown",
		},
		{
			name: "config map with flannel in data",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cni-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"config.conf": "flannel network configuration",
					},
				},
			},
			want: "flannel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)

			got, err := detectCNIPlugin(context.Background(), client)
			if err != nil {
				t.Errorf("detectCNIPlugin() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("detectCNIPlugin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "exact match",
			s:      "calico",
			substr: "calico",
			want:   true,
		},
		{
			name:   "prefix match",
			s:      "calico-config",
			substr: "calico",
			want:   true,
		},
		{
			name:   "suffix match",
			s:      "use-calico",
			substr: "calico",
			want:   true,
		},
		{
			name:   "middle match",
			s:      "use-calico-here",
			substr: "calico",
			want:   true,
		},
		{
			name:   "no match",
			s:      "cilium",
			substr: "calico",
			want:   false,
		},
		{
			name:   "empty substring",
			s:      "calico",
			substr: "",
			want:   true,
		},
		{
			name:   "empty string",
			s:      "",
			substr: "calico",
			want:   false,
		},
		{
			name:   "substring longer than string",
			s:      "cal",
			substr: "calico",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.s, tt.substr); got != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

func TestFindSubstring(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "substring found",
			s:      "hello world",
			substr: "world",
			want:   true,
		},
		{
			name:   "substring at start",
			s:      "hello world",
			substr: "hello",
			want:   true,
		},
		{
			name:   "substring at end",
			s:      "hello world",
			substr: "world",
			want:   true,
		},
		{
			name:   "substring not found",
			s:      "hello world",
			substr: "foo",
			want:   false,
		},
		{
			name:   "empty substring",
			s:      "hello",
			substr: "",
			want:   true,
		},
		{
			name:   "empty string",
			s:      "",
			substr: "hello",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findSubstring(tt.s, tt.substr); got != tt.want {
				t.Errorf("findSubstring(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}