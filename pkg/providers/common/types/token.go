/*
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

package types

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GenerateBootstrapToken creates a bootstrap token for node registration
func GenerateBootstrapToken(ctx context.Context, client kubernetes.Interface, ttl time.Duration) (string, error) {
	// Generate token ID (6 characters)
	tokenID, err := generateRandomString(6)
	if err != nil {
		return "", fmt.Errorf("generating token ID: %w", err)
	}

	// Generate token secret (16 characters)
	tokenSecret, err := generateRandomString(16)
	if err != nil {
		return "", fmt.Errorf("generating token secret: %w", err)
	}

	// Create bootstrap token secret
	tokenName := fmt.Sprintf("bootstrap-token-%s", tokenID)
	tokenSecret = fmt.Sprintf("%s.%s", tokenID, tokenSecret)

	// Calculate expiration time
	expiration := time.Now().Add(ttl)

	// Create the bootstrap token secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenName,
			Namespace: "kube-system",
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte(tokenID),
			"token-secret": []byte(tokenSecret[7:]), // Remove the "tokenID." prefix
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":         []byte("true"),
			"auth-extra-groups":               []byte("system:bootstrappers:karpenter:default-node-pool"),
		},
	}

	// Add expiration if specified
	if ttl > 0 {
		secret.Data["expiration"] = []byte(expiration.Format(time.RFC3339))
	}

	// Create the secret
	_, err = client.CoreV1().Secrets("kube-system").Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("creating bootstrap token secret: %w", err)
	}

	return tokenSecret, nil
}

// FindOrCreateBootstrapToken finds an existing bootstrap token or creates a new one
func FindOrCreateBootstrapToken(ctx context.Context, client kubernetes.Interface, ttl time.Duration) (string, error) {
	// Look for existing Karpenter bootstrap tokens
	secrets, err := client.CoreV1().Secrets("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "karpenter.sh/bootstrap-token=true",
	})
	if err != nil {
		return "", fmt.Errorf("listing bootstrap tokens: %w", err)
	}

	// Check if we have a valid existing token
	for _, secret := range secrets.Items {
		if secret.Type == "bootstrap.kubernetes.io/token" {
			// Check if token is still valid
			if expirationBytes, exists := secret.Data["expiration"]; exists {
				if expiration, parseErr := time.Parse(time.RFC3339, string(expirationBytes)); parseErr == nil {
					if time.Now().Before(expiration.Add(-time.Hour)) { // Renew 1 hour before expiration
						tokenID := string(secret.Data["token-id"])
						tokenSecret := string(secret.Data["token-secret"])
						return fmt.Sprintf("%s.%s", tokenID, tokenSecret), nil
					}
				}
			}
		}
	}

	// No valid token found, create a new one
	return GenerateBootstrapToken(ctx, client, ttl)
}

// GetInternalAPIServerEndpoint discovers the API server endpoint for node bootstrapping
func GetInternalAPIServerEndpoint(ctx context.Context, client kubernetes.Interface) (string, error) {
	// Get the kubeadm-config ConfigMap which contains the actual API endpoint
	kubeadmConfig, err := client.CoreV1().ConfigMaps("kube-system").Get(ctx, "kubeadm-config", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting kubeadm-config configmap: %w", err)
	}

	// Parse ClusterConfiguration to find the controlPlaneEndpoint
	clusterConfig, exists := kubeadmConfig.Data["ClusterConfiguration"]
	if !exists {
		return "", fmt.Errorf("ClusterConfiguration not found in kubeadm-config")
	}

	// Look for controlPlaneEndpoint in the YAML
	lines := strings.Split(clusterConfig, "\n")
	for _, line := range lines {
		if strings.Contains(line, "controlPlaneEndpoint:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 3 {
				// Extract host:port (parts[1] has the host, parts[2] has the port)
				host := strings.TrimSpace(parts[1])
				port := strings.TrimSpace(parts[2])
				return fmt.Sprintf("https://%s:%s", host, port), nil
			}
		}
	}

	return "", fmt.Errorf("controlPlaneEndpoint not found in kubeadm-config")
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes)[:length], nil
}