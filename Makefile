# Tool versions — single source of truth for local + CI
CONTROLLER_GEN_VERSION ?= v0.20.1
# A full release tag, not a minor: the install script the lint job uses fetches a
# GitHub release, and there is no release called v2.11.
GOLANGCI_LINT_VERSION ?= v2.13.2
GITLEAKS_VERSION ?= v8.30.1
SETUP_ENVTEST_VERSION ?= v0.25.1
# Lints the OpenAPI specs; run through npx, so it needs Node but no install.
REDOCLY_CLI_VERSION ?= 2.54.2
# The Kubernetes version integration tests run their API server at.
ENVTEST_K8S_VERSION ?= 1.34.1

BINARIES := mcp-gateway-controller mcp-gateway mcp-gateway-proxy mcp-gateway-ui

.PHONY: secret-scan secret-scan-history generate generate-api lint-api generate-crds build test integration-test dev-stack lint clean ui-install ui-build ui-test ui-lint ui-clean build-binaries build-all changelog helm-docs helm-lint helm-package install-tools $(addprefix build-,$(BINARIES))

# The console's web UI is an Angular application in web/ui. It builds into
# internal/ui/dist/ui, which mcp-gateway-ui embeds: `go build` compiles in
# whatever that directory holds, so a build without `make ui-build` produces a
# console that passes the API through and says it has no UI. The image and the
# release build it first. Its API client is generated from
# api/openapi/inspect.yaml on every build, lint and test.
UI_DIR := web/ui

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

# Everything the console needs, on this machine, until Ctrl-C: an API server
# with the CRDs, MCP backends with example resources, the controller, the
# gateway with its inspect API, and mcp-gateway-ui at http://127.0.0.1:8480/.
dev-stack: ui-build
	DEVSTACK=1 KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION) use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./tests/integration/ -run TestDevStack -count=1 -v -timeout 0

lint:
	golangci-lint run ./...

clean: ui-clean
	rm -rf bin/

ui-install:
	cd $(UI_DIR) && npm ci

# `npm ci` is skipped when node_modules is already there: it takes minutes, the
# build seconds.
$(UI_DIR)/node_modules:
	$(MAKE) ui-install

ui-build: $(UI_DIR)/node_modules
	cd $(UI_DIR) && npm run build

ui-test: $(UI_DIR)/node_modules
	cd $(UI_DIR) && npm test

ui-lint: $(UI_DIR)/node_modules
	cd $(UI_DIR) && npm run lint

ui-clean:
	rm -rf internal/ui/dist/ui

# Build one binary for the current platform (quick dev build)
$(addprefix build-,$(BINARIES)): build-%:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/$* ./cmd/$*

# The console carries the web UI, so its binary is only complete with a UI build.
build-mcp-gateway-ui: ui-build

build-binaries: $(addprefix build-,$(BINARIES))

# Cross-compile all binaries for release platforms
build-all: ui-build
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

# Generate the inspect API's types and server interface from its OpenAPI spec.
# The spec is the source of truth; TestGeneratedCodeIsCurrent fails when the
# generated code does not match it. oapi-codegen is a tool in go.mod.
generate-api:
	cd internal/inspect/api && go generate ./...

lint-api:
	npx -y @redocly/cli@$(REDOCLY_CLI_VERSION) lint api/openapi/inspect.yaml

# Install pinned tool versions
install-tools:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Generate CRD manifests from the API types into charts/mcp-gateway/crds/.
generate-crds:
	@mkdir -p charts/mcp-gateway/crds
	controller-gen crd:generateEmbeddedObjectMeta=true \
		paths="./api/v1alpha1/..." \
		output:crd:artifacts:config=charts/mcp-gateway/crds
	@echo "Done. CRDs updated in charts/mcp-gateway/crds/"

# Generate Helm chart documentation (README.md) via helm-docs
helm-docs:
	helm-docs --chart-search-root charts/

# Lint both Helm charts. The mcp-gateway chart refuses to render without token
# verification settings, so it is linted with the values CI uses.
helm-lint:
	helm lint charts/mcp-gateway -f charts/mcp-gateway/ci/default-values.yaml
	helm lint charts/mcp-gateway-grafana-dashboard

# Package both Helm charts
helm-package:
	helm package charts/mcp-gateway --destination bin/
	helm package charts/mcp-gateway-grafana-dashboard --destination bin/

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
