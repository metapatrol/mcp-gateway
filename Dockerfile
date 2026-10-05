# One Dockerfile for every binary; BINARY picks which one an image holds.
# Separate images rather than one with all: the proxy runs as a sidecar next to
# agents, and has no business shipping the gateway and the controller into
# every pod it joins; the console's image is the only one with a web UI in it.
#
#   podman build --build-arg BINARY=mcp-gateway -t .../mcp-gateway .
# mcp-gateway-ui carries the console's web UI, compiled into the binary, so that
# stage runs first. For the other binaries it only creates the empty directory
# the embed directive needs, which costs one layer and no npm install. The UI's
# API client is generated from the inspect API's spec as part of its build.
#
# The console's packages come from the forge's npm registry, so building
# mcp-gateway-ui needs the npm_token secret (see the ui stage) and the forge
# within reach; the other binaries need neither. The GitHub mirror points
# web/ui/.npmrc at GitHub Packages instead (.github/workflows/publish.yml).
#
# Public base images, so that a build works anywhere. A build that should pull
# through a registry mirror passes its own:
#
#   --build-arg NODE_IMAGE=registry.example.com/library/node:24-alpine
#   --build-arg GO_IMAGE=registry.example.com/library/golang:1.27-alpine
ARG NODE_IMAGE=docker.io/library/node:24-alpine
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine

FROM ${NODE_IMAGE} AS ui

ARG BINARY

WORKDIR /src
COPY web/ui/package.json web/ui/package-lock.json web/ui/.npmrc ./web/ui/
# The console's @metapatrol packages come from the forge's npm registry: a
# token with read:package scope as the secret npm_token, and the platform CA as
# platform_ca if the build host does not trust it
# (podman build --secret id=npm_token,src=... --secret id=platform_ca,src=...).
# Secrets, not build arguments, so neither ends up in the image.
RUN --mount=type=secret,id=npm_token --mount=type=secret,id=platform_ca \
    if [ "$BINARY" = "mcp-gateway-ui" ]; then \
      if [ -s /run/secrets/platform_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/platform_ca; fi; \
      cd web/ui && NPM_TOKEN="$(cat /run/secrets/npm_token 2>/dev/null)" npm ci --no-audit --no-fund; \
    fi

COPY api/openapi ./api/openapi
COPY web/ui ./web/ui
RUN mkdir -p internal/ui/dist && touch internal/ui/dist/.gitkeep && \
    if [ "$BINARY" = "mcp-gateway-ui" ]; then cd web/ui && npm run build; fi

FROM ${GO_IMAGE} AS builder

ARG BINARY
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

RUN test -n "$BINARY" || (echo "--build-arg BINARY=mcp-gateway{,-controller,-proxy,-ui} is required" && exit 1)

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The web UI, built above. It replaces the placeholder directory the repository
# carries, and go:embed takes it from there.
COPY --from=ui /src/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /app ./cmd/${BINARY}

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app /app
ENTRYPOINT ["/app"]
