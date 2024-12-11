# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
BINARY_NAME=karpenter-ibm-cloud

# Build parameters
BUILD_DIR=bin
MAIN_PKG=cmd/controller/main.go

.PHONY: all build clean test deps generate manifests vendor

all: deps vendor generate manifests build test

build:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PKG)

clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)

test:
	$(GOTEST) -v ./...

deps:
	$(GOMOD) download
	$(GOMOD) tidy

vendor:
	$(GOMOD) tidy
	$(GOMOD) vendor
	$(GOMOD) verify

# Generate code
generate:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.3
	controller-gen object paths=./pkg/apis/v1

# Generate CRD manifests
manifests:
	controller-gen \
		crd \
		paths="./pkg/apis/..." \
		output:crd:artifacts:config=config/crds
	# Copy CRDs to Helm charts directory
	mkdir -p charts/crds
	cp config/crds/* charts/crds/

# Run code generators
codegen: vendor generate manifests
