# Local equivalents of the CI pipeline stages. Scan targets use tools that
# are installed locally and skip (with a message) when a tool is missing.

IMAGE ?= ghcr.io/rakaprad/secure-ship
TAG   ?= dev

.DEFAULT_GOAL := help

# Run a command only if its tool is on PATH; otherwise print a skip notice.
define run-if-present
	@if command -v $(1) >/dev/null 2>&1; then echo "==> $(2)"; $(2); \
	else echo "--> skipping: $(1) not installed"; fi
endef

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the server on :8080
	go run ./cmd/server

.PHONY: build
build: ## Build a static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

.PHONY: test
test: ## Run unit tests with the race detector and coverage
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: lint
lint: ## gofmt check, go vet, golangci-lint (incl. gosec)
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...
	$(call run-if-present,golangci-lint,golangci-lint run ./...)

.PHONY: scan
scan: ## Secrets, dependency CVEs, and IaC misconfiguration scans
	$(call run-if-present,gitleaks,gitleaks git --redact .)
	$(call run-if-present,govulncheck,govulncheck ./...)
	@mkdir -p .rendered
	kubectl kustomize deploy/k8s/overlays/dev  > .rendered/dev.yaml
	kubectl kustomize deploy/k8s/overlays/prod > .rendered/prod.yaml
	$(call run-if-present,trivy,trivy config --exit-code 1 --severity HIGH$(,)CRITICAL .rendered)
	$(call run-if-present,trivy,trivy config --exit-code 1 --severity HIGH$(,)CRITICAL Dockerfile)
	@rm -rf .rendered

.PHONY: k8s
k8s: ## Render both Kustomize overlays
	kubectl kustomize deploy/k8s/overlays/dev
	kubectl kustomize deploy/k8s/overlays/prod

.PHONY: image
image: ## Build the container image (requires Docker)
	docker build -t $(IMAGE):$(TAG) .

.PHONY: image-scan
image-scan: image ## Build and scan the image with Trivy
	$(call run-if-present,trivy,trivy image --exit-code 1 --severity HIGH$(,)CRITICAL --ignore-unfixed $(IMAGE):$(TAG))

.PHONY: clean
clean: ## Remove build and test artifacts
	rm -rf bin coverage.out .rendered

, := ,
