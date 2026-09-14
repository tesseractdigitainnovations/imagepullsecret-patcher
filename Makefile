# imagepullsecret-patcher development tasks.

BINARY      := imagepullsecret-patcher
PKG         := ./cmd/$(BINARY)

# Everything about the image is derived from the git remote, so a fork builds
# and pushes to its own registry namespace without editing this file.
# Override any of these on the command line, e.g. `make docker REGISTRY=docker.io`.
REPO_SLUG   ?= $(shell git remote get-url origin 2>/dev/null \
                 | sed -E -e 's#^(git@|ssh://git@|https://)##' -e 's#^[^/:]+[:/]##' -e 's#\.git$$##' \
                 | tr '[:upper:]' '[:lower:]')
REGISTRY    ?= ghcr.io
IMAGE       ?= $(REGISTRY)/$(if $(REPO_SLUG),$(REPO_SLUG),local/$(BINARY))
SOURCE_URL  ?= $(if $(REPO_SLUG),https://github.com/$(REPO_SLUG),)
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse HEAD 2>/dev/null)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORMS   ?= linux/amd64,linux/arm64,linux/arm/v7

BUILD_ARGS := \
	--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg BUILD_DATE=$(BUILD_DATE) \
	--build-arg IMAGE_TITLE=$(BINARY) \
	--build-arg IMAGE_SOURCE=$(SOURCE_URL) \
	--build-arg IMAGE_URL=$(SOURCE_URL)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(BUILD_DATE)

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary for the host platform into dist/
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY) $(PKG)

.PHONY: run
run: ## Run the patcher against the current kubectl context, once
	go run $(PKG) -runonce -debug $(ARGS)

.PHONY: test
test: ## Run the tests with the race detector and a coverage profile
	go test -race -covermode=atomic -coverprofile=coverage.txt ./...

.PHONY: cover
cover: test ## Show per-function test coverage
	go tool cover -func=coverage.txt

.PHONY: bench
bench: ## Run the benchmarks
	go test -bench=. -benchmem ./...

.PHONY: lint
lint: ## Run golangci-lint (see .golangci.yml)
	golangci-lint run

.PHONY: fmt
fmt: ## Format the code and tidy the module
	go fmt ./...
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check
check: fmt vet lint test ## Everything CI runs

.PHONY: image-name
image-name: ## Print the image reference that the docker targets build
	@echo $(IMAGE):$(VERSION)

.PHONY: docker
docker: ## Build the image for the host platform and load it into the local daemon
	docker buildx build --load $(BUILD_ARGS) -t $(IMAGE):$(VERSION) .

.PHONY: docker-multiarch
docker-multiarch: ## Build the image for every published platform (add PUSH=1 to push it)
	docker buildx build \
		--platform $(PLATFORMS) \
		$(if $(PUSH),--push,) \
		$(BUILD_ARGS) \
		-t $(IMAGE):$(VERSION) \
		.

.PHONY: deploy-example
deploy-example: ## Apply the example manifests to the current kubectl context
	kubectl apply -f deploy-example/kubernetes-manifest/

.PHONY: clean
clean: ## Remove build and coverage output
	rm -rf dist coverage.txt
