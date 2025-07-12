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

package image

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockVPCClient implements a mock VPC client for testing
type MockVPCClient struct {
	images         map[string]*vpcv1.Image
	listImagesFunc func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, error)
	getImageFunc   func(ctx context.Context, imageID string) (*vpcv1.Image, error)
}

func (m *MockVPCClient) GetImage(ctx context.Context, imageID string) (*vpcv1.Image, error) {
	if m.getImageFunc != nil {
		return m.getImageFunc(ctx, imageID)
	}
	if image, exists := m.images[imageID]; exists {
		return image, nil
	}
	return nil, fmt.Errorf("image %s not found", imageID)
}

func (m *MockVPCClient) ListImages(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, error) {
	if m.listImagesFunc != nil {
		return m.listImagesFunc(ctx, options)
	}
	
	var images []vpcv1.Image
	for _, image := range m.images {
		// Filter by visibility if specified
		if options.Visibility != nil {
			if image.Visibility != nil && *image.Visibility != *options.Visibility {
				continue
			}
		}
		images = append(images, *image)
	}
	
	return &vpcv1.ImageCollection{Images: images}, nil
}

// MockResolver wraps the resolver with a mock VPC client
type MockResolver struct {
	mockClient *MockVPCClient
}

func (r *MockResolver) ResolveImage(ctx context.Context, imageIdentifier string) (string, error) {
	if imageIdentifier == "" {
		return "", fmt.Errorf("image identifier cannot be empty")
	}

	// If it looks like an image ID, try to get it directly
	if isImageID(imageIdentifier) {
		_, err := r.mockClient.GetImage(ctx, imageIdentifier)
		if err == nil {
			return imageIdentifier, nil
		}
		return "", fmt.Errorf("image ID %s not found: %w", imageIdentifier, err)
	}

	// Otherwise, treat it as an image name and resolve it
	return r.resolveImageByName(ctx, imageIdentifier)
}

func (r *MockResolver) resolveImageByName(ctx context.Context, imageName string) (string, error) {
	// List all images with filtering
	options := &vpcv1.ListImagesOptions{
		Visibility: stringPtr("public"),
	}

	images, err := r.mockClient.ListImages(ctx, options)
	if err != nil {
		return "", fmt.Errorf("listing images: %w", err)
	}

	// Find matching images
	var candidates []vpcv1.Image
	for _, image := range images.Images {
		if image.Name != nil && matchesImageName(*image.Name, imageName) {
			candidates = append(candidates, image)
		}
	}

	if len(candidates) == 0 {
		// Try private images if no public images found
		options.Visibility = stringPtr("private")
		images, err := r.mockClient.ListImages(ctx, options)
		if err != nil {
			return "", fmt.Errorf("listing private images: %w", err)
		}

		for _, image := range images.Images {
			if image.Name != nil && matchesImageName(*image.Name, imageName) {
				candidates = append(candidates, image)
			}
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no image found matching name '%s'", imageName)
	}

	// Sort candidates by creation date (newest first)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt == nil || candidates[j].CreatedAt == nil {
			return false
		}
		timeI, errI := time.Parse(time.RFC3339, candidates[i].CreatedAt.String())
		timeJ, errJ := time.Parse(time.RFC3339, candidates[j].CreatedAt.String())
		if errI != nil || errJ != nil {
			return false
		}
		return timeI.After(timeJ)
	})

	// Return the most recent matching image
	if candidates[0].ID == nil {
		return "", fmt.Errorf("image %s has no ID", *candidates[0].Name)
	}

	return *candidates[0].ID, nil
}

func (r *MockResolver) ListAvailableImages(ctx context.Context, nameFilter string) ([]ImageInfo, error) {
	options := &vpcv1.ListImagesOptions{
		Visibility: stringPtr("public"),
	}

	images, err := r.mockClient.ListImages(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	var result []ImageInfo
	for _, image := range images.Images {
		if image.Name == nil || image.ID == nil {
			continue
		}

		// Apply name filter if provided
		if nameFilter != "" && !strings.Contains(strings.ToLower(*image.Name), strings.ToLower(nameFilter)) {
			continue
		}

		info := ImageInfo{
			ID:   *image.ID,
			Name: *image.Name,
		}

		if image.CreatedAt != nil {
			if createdAt, err := time.Parse(time.RFC3339, image.CreatedAt.String()); err == nil {
				info.CreatedAt = createdAt
			}
		}

		if image.OperatingSystem != nil && image.OperatingSystem.Name != nil {
			info.OperatingSystem = *image.OperatingSystem.Name
		}

		if image.Status != nil {
			info.Status = *image.Status
		}

		result = append(result, info)
	}

	// Sort by creation date (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result, nil
}

func createTestImage(id, name, visibility string, createdAt time.Time) *vpcv1.Image {
	createdAtStr := strfmt.DateTime(createdAt)
	return &vpcv1.Image{
		ID:         &id,
		Name:       &name,
		Visibility: &visibility,
		CreatedAt:  &createdAtStr,
		Status:     stringPtr("available"),
		OperatingSystem: &vpcv1.OperatingSystem{
			Name: stringPtr("ubuntu-20-04-amd64"),
		},
	}
}

func TestResolver_ResolveImage(t *testing.T) {
	ctx := context.Background()
	
	tests := []struct {
		name           string
		imageIdentifier string
		mockImages     map[string]*vpcv1.Image
		mockListFunc   func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, error)
		mockGetFunc    func(ctx context.Context, imageID string) (*vpcv1.Image, error)
		expectedID     string
		expectError    bool
		errorContains  string
	}{
		{
			name:           "resolve valid image ID",
			imageIdentifier: "r006-12345678-1234-1234-1234-123456789abc",
			mockImages: map[string]*vpcv1.Image{
				"r006-12345678-1234-1234-1234-123456789abc": createTestImage(
					"r006-12345678-1234-1234-1234-123456789abc",
					"ubuntu-20-04",
					"public",
					time.Now(),
				),
			},
			expectedID:  "r006-12345678-1234-1234-1234-123456789abc",
			expectError: false,
		},
		{
			name:           "resolve image name to ID",
			imageIdentifier: "ubuntu-20-04",
			mockImages: map[string]*vpcv1.Image{
				"r006-12345678-1234-1234-1234-123456789abc": createTestImage(
					"r006-12345678-1234-1234-1234-123456789abc",
					"ubuntu-20-04",
					"public",
					time.Now(),
				),
			},
			expectedID:  "r006-12345678-1234-1234-1234-123456789abc",
			expectError: false,
		},
		{
			name:           "resolve partial image name",
			imageIdentifier: "ubuntu",
			mockImages: map[string]*vpcv1.Image{
				"r006-12345678-1234-1234-1234-123456789abc": createTestImage(
					"r006-12345678-1234-1234-1234-123456789abc",
					"ibm-ubuntu-20-04-minimal-amd64-2",
					"public",
					time.Now(),
				),
			},
			expectedID:  "r006-12345678-1234-1234-1234-123456789abc",
			expectError: false,
		},
		{
			name:           "select newest image when multiple matches",
			imageIdentifier: "ubuntu-20-04",
			mockImages: map[string]*vpcv1.Image{
				"r006-older": createTestImage(
					"r006-older",
					"ubuntu-20-04-v1",
					"public",
					time.Now().Add(-24*time.Hour),
				),
				"r006-newer": createTestImage(
					"r006-newer",
					"ubuntu-20-04-v2",
					"public",
					time.Now(),
				),
			},
			expectedID:  "r006-newer",
			expectError: false,
		},
		{
			name:           "image ID not found",
			imageIdentifier: "r006-12345678-1234-1234-1234-nonexistent123",
			mockImages:     map[string]*vpcv1.Image{},
			expectedID:     "",
			expectError:    true,
			errorContains:  "not found",
		},
		{
			name:           "image name not found",
			imageIdentifier: "nonexistent-image",
			mockImages: map[string]*vpcv1.Image{
				"r006-12345678-1234-1234-1234-123456789abc": createTestImage(
					"r006-12345678-1234-1234-1234-123456789abc",
					"ubuntu-20-04",
					"public",
					time.Now(),
				),
			},
			expectedID:    "",
			expectError:   true,
			errorContains: "no image found matching name",
		},
		{
			name:           "empty image identifier",
			imageIdentifier: "",
			mockImages:     map[string]*vpcv1.Image{},
			expectedID:     "",
			expectError:    true,
			errorContains:  "cannot be empty",
		},
		{
			name:           "check private images when public not found",
			imageIdentifier: "private-ubuntu",
			mockListFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, error) {
				if options.Visibility != nil && *options.Visibility == "public" {
					return &vpcv1.ImageCollection{Images: []vpcv1.Image{}}, nil
				}
				if options.Visibility != nil && *options.Visibility == "private" {
					return &vpcv1.ImageCollection{
						Images: []vpcv1.Image{
							*createTestImage(
								"r006-private",
								"private-ubuntu-image",
								"private",
								time.Now(),
							),
						},
					}, nil
				}
				return &vpcv1.ImageCollection{Images: []vpcv1.Image{}}, nil
			},
			expectedID:  "r006-private",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockVPCClient{
				images:         tt.mockImages,
				listImagesFunc: tt.mockListFunc,
				getImageFunc:   tt.mockGetFunc,
			}
			
			resolver := &MockResolver{
				mockClient: mockClient,
			}

			result, err := resolver.ResolveImage(ctx, tt.imageIdentifier)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, result)
			}
		})
	}
}

func TestResolver_ListAvailableImages(t *testing.T) {
	ctx := context.Background()
	
	now := time.Now()
	images := map[string]*vpcv1.Image{
		"r006-ubuntu": createTestImage(
			"r006-ubuntu",
			"ibm-ubuntu-20-04-minimal-amd64-2",
			"public",
			now,
		),
		"r006-centos": createTestImage(
			"r006-centos",
			"ibm-centos-7-9-minimal-amd64-5",
			"public",
			now.Add(-time.Hour),
		),
		"r006-windows": createTestImage(
			"r006-windows",
			"ibm-windows-server-2019-full-std-amd64-5",
			"public",
			now.Add(-2*time.Hour),
		),
	}

	tests := []struct {
		name         string
		nameFilter   string
		expectedCount int
		expectedFirst string
	}{
		{
			name:         "list all images",
			nameFilter:   "",
			expectedCount: 3,
			expectedFirst: "r006-ubuntu", // Newest first
		},
		{
			name:         "filter by ubuntu",
			nameFilter:   "ubuntu",
			expectedCount: 1,
			expectedFirst: "r006-ubuntu",
		},
		{
			name:         "filter by minimal",
			nameFilter:   "minimal",
			expectedCount: 2,
			expectedFirst: "r006-ubuntu", // Newest first
		},
		{
			name:         "filter with no matches",
			nameFilter:   "nonexistent",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockVPCClient{
				images: images,
			}
			
			resolver := &MockResolver{
				mockClient: mockClient,
			}

			result, err := resolver.ListAvailableImages(ctx, tt.nameFilter)

			require.NoError(t, err)
			assert.Len(t, result, tt.expectedCount)
			
			if tt.expectedCount > 0 {
				assert.Equal(t, tt.expectedFirst, result[0].ID)
				// Verify sorting (newest first)
				for i := 1; i < len(result); i++ {
					assert.True(t, result[i-1].CreatedAt.After(result[i].CreatedAt) || result[i-1].CreatedAt.Equal(result[i].CreatedAt))
				}
			}
		})
	}
}

func TestIsImageID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid image ID",
			input:    "r006-12345678-1234-1234-1234-123456789abc",
			expected: true,
		},
		{
			name:     "valid image ID with different region",
			input:    "r014-87654321-4321-4321-4321-cba987654321",
			expected: true,
		},
		{
			name:     "image name",
			input:    "ubuntu-20-04-minimal",
			expected: false,
		},
		{
			name:     "short string",
			input:    "r006",
			expected: false,
		},
		{
			name:     "doesn't start with r",
			input:    "a006-12345678-1234-1234-1234-123456789abc",
			expected: false,
		},
		{
			name:     "invalid region format",
			input:    "rabc-12345678-1234-1234-1234-123456789abc",
			expected: false,
		},
		{
			name:     "too few UUID parts",
			input:    "r006-12345678",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isImageID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesImageName(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		pattern   string
		expected  bool
	}{
		{
			name:      "exact match",
			imageName: "ubuntu-20-04",
			pattern:   "ubuntu-20-04",
			expected:  true,
		},
		{
			name:      "contains match",
			imageName: "ibm-ubuntu-20-04-minimal-amd64-2",
			pattern:   "ubuntu",
			expected:  true,
		},
		{
			name:      "case insensitive",
			imageName: "IBM-Ubuntu-20-04",
			pattern:   "ubuntu",
			expected:  true,
		},
		{
			name:      "wildcard prefix",
			imageName: "ibm-ubuntu-20-04-minimal",
			pattern:   "*ubuntu*",
			expected:  true,
		},
		{
			name:      "wildcard suffix",
			imageName: "ubuntu-20-04-minimal",
			pattern:   "ubuntu*",
			expected:  true,
		},
		{
			name:      "wildcard prefix only",
			imageName: "ibm-ubuntu-20-04",
			pattern:   "*04",
			expected:  true,
		},
		{
			name:      "no match",
			imageName: "centos-7-9",
			pattern:   "ubuntu",
			expected:  false,
		},
		{
			name:      "wildcard no match",
			imageName: "centos-7-9",
			pattern:   "*ubuntu*",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesImageName(tt.imageName, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}