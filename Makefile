# Image URL to use all building/pushing image targets
IMG ?= honeycombio/kspan:dev

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run unit tests
test: fmt vet
	go test ./... -coverprofile cover.out

# Run the Tier-1 e2e test: drives the controller and asserts on the spans it
# emits over a real OTLP/gRPC round-trip into an in-process sink. Deterministic
# (mtime-driven playback), no external cluster required. See KSPAN-E2E-RESEARCH.md.
test-e2e: fmt
	go test -tags e2e -run TestOTLPExportE2E ./controllers/events/

# Build manager binary
manager:
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: fmt vet
	go run ./main.go

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Build the docker image
docker-build: test
	docker build . -t ${IMG}

# Push the docker image
docker-push: docker-build
	docker push ${IMG}
