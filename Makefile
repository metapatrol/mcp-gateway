# Tool versions — single source of truth for local + CI
CONTROLLER_GEN_VERSION ?= v0.20.1
# A full release tag, not a minor: the install script the lint job uses fetches a
# GitHub release, and there is no release called v2.11.
GOLANGCI_LINT_VERSION ?= v2.11.4
GITLEAKS_VERSION ?= v8.30.1
SETUP_ENVTEST_VERSION ?= v0.25.1
# The Kubernetes version integration tests run their API server at.
ENVTEST_K8S_VERSION ?= 1.34.1

BINARIES := mcp-gate-controller mcp-gate-gateway mcp-gate-proxy

.PHONY: secret-scan secret-scan-history generate generate-crds build test integration-test lint clean build-binaries build-all changelog helm-docs helm-lint helm-package install-tools $(addprefix build-,$(BINARIES))

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build ./...

test:
	go test -v -count=1 ./...

# The controller and the gateway against a real API server with the CRDs
# installed: schema validation, status subresources and watches, which a fake
# client does not have. setup-envtest downloads the API server and etcd once.
integration-test:
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION) use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./tests/integration/... -v -count=1 -timeout 5m

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

# Build one binary for the current platform (quick dev build)
$(addprefix build-,$(BINARIES)): build-%:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/$* ./cmd/$*

build-binaries: $(addprefix build-,$(BINARIES))

# Cross-compile all binaries for release platforms
build-all:
	@mkdir -p bin
	@for bin in $(BINARIES); do \
		for arch in amd64 arm64; do \
			echo "Building $$bin linux/$$arch..."; \
			CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build \
				-ldflags="$(LDFLAGS)" \
				-o bin/$$bin-linux-$$arch ./cmd/$$bin; \
		done; \
	done

# Generate deepcopy
generate:
	controller-gen object paths="./..."

# Install pinned tool versions
install-tools:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Generate CRD manifests from the API types into charts/mcp-gate/crds/.
generate-crds:
	@mkdir -p charts/mcp-gate/crds
	controller-gen crd:generateEmbeddedObjectMeta=true \
		paths="./api/v1alpha1/..." \
		output:crd:artifacts:config=charts/mcp-gate/crds
	@echo "Done. CRDs updated in charts/mcp-gate/crds/"

# Generate Helm chart documentation (README.md) via helm-docs
helm-docs:
	helm-docs --chart-search-root charts/

# Lint both Helm charts. The mcp-gate chart refuses to render without token
# verification settings, so it is linted with the values CI uses.
helm-lint:
	helm lint charts/mcp-gate -f charts/mcp-gate/ci/default-values.yaml
	helm lint charts/mcp-gate-grafana-dashboard

# Package both Helm charts
helm-package:
	helm package charts/mcp-gate --destination bin/
	helm package charts/mcp-gate-grafana-dashboard --destination bin/

# Generate CHANGELOG.md from conventional commits
changelog:
	git-cliff --config cliff.toml -o CHANGELOG.md

# gitleaks, downloaded rather than compiled: it is a static binary and the
# release tarball is seconds where `go install` is a minute of dependencies.
bin/gitleaks:
	@mkdir -p bin
	curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/$(GITLEAKS_VERSION)/gitleaks_$(GITLEAKS_VERSION:v%=%)_linux_x64.tar.gz" \
		| tar xz -C bin gitleaks
	@chmod +x bin/gitleaks

# What ci.yml runs. The working tree, so the result depends only on the commit
# under test and not on how much history the checkout happened to fetch.
secret-scan: bin/gitleaks
	bin/gitleaks dir . --no-banner --redact

# The whole history, for an audit. Deliberately not in CI: a leak in an old
# commit would turn every run red until the history is rewritten, which is a
# decision to take once and not a gate on unrelated work.
secret-scan-history: bin/gitleaks
	bin/gitleaks git . --no-banner --redact
