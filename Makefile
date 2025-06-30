# Copyright 2024 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
VERSION ?= $(shell git describe --tags --always --dirty)

# Build settings
BINARY_NAME = karpenter-ibm-controller
BUILD_DIR = bin
PLATFORMS = linux/amd64 linux/arm64

# Build flags
LDFLAGS = -ldflags "-X main.version=${VERSION}"
CGO_ENABLED = 0

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29
ENVTEST = go run ${PROJECT_DIR}/vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest

GINKGO = go run ${PROJECT_DIR}/vendor/github.com/onsi/ginkgo/v2/ginkgo
GINKGO_ARGS = -v --randomize-all --randomize-suites --keep-going --race --trace --timeout=30m

CONTROLLER_GEN = ~/.local/share/go/bin/controller-gen

.PHONY: all
all: build

.PHONY: build
build: generate ## Build binary for current platform
	CGO_ENABLED=$(CGO_ENABLED) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) cmd/controller/main.go

.PHONY: build-multiarch
build-multiarch: generate ## Build binaries for all supported platforms
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		echo "Building for $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=$(CGO_ENABLED) \
			go build $(LDFLAGS) \
			-o $(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch \
			cmd/controller/main.go || exit 1; \
	done

.PHONY: help
help: ## Display help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: gen-objects
gen-objects: ## generate the controller-gen related objects
	$(CONTROLLER_GEN) object paths="./..."

.PHONY: generate
generate: gen-objects manifests ## generate all controller-gen files

.PHONY: manifests
manifests: ## generate the controller-gen kubernetes manifests
	@echo "Generating CRDs directly to Helm chart..."
	$(CONTROLLER_GEN) crd paths="./pkg/apis/v1alpha1" output:crd:artifacts:config=charts/crds
	$(CONTROLLER_GEN) crd paths="./vendor/sigs.k8s.io/karpenter/pkg/apis/..." output:crd:artifacts:config=charts/crds
	@echo "Generating RBAC manifests..."
	@rm -f charts/templates/rbac_*.yaml charts/templates/role_*.yaml charts/templates/clusterrole_*.yaml
	GOFLAGS="-mod=mod" $(CONTROLLER_GEN) rbac:roleName=karpenter-manager paths="./pkg/controllers" output:rbac:dir=charts/templates

.PHONY: test
test: vendor unit

.PHONY: unit
unit: 
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./vendor/sigs.k8s.io/cluster-api/api/v1beta1/..." output:crd:artifacts:config=vendor/sigs.k8s.io/cluster-api/api/v1beta1
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(PROJECT_DIR)/bin)" ${GINKGO} ${GINKGO_ARGS} ${GINKGO_EXTRA_ARGS} ./...

.PHONY: vendor
vendor: ## update modules and populate local vendor directory
	go mod tidy
	go mod vendor
	go mod verify

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
