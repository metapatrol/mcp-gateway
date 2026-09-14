# One Dockerfile for all three binaries; BINARY picks which one an image holds.
# Separate images rather than one with all three: the proxy runs as a sidecar
# next to agents, and has no business shipping the gateway and the controller
# into every pod it joins.
#
#   podman build --build-arg BINARY=mcp-gate-gateway -t .../mcp-gate-gateway .
FROM zot.metapatrol.local/library/golang:1.26-alpine AS builder

ARG BINARY
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

RUN test -n "$BINARY" || (echo "--build-arg BINARY=mcp-gate-{controller,gateway,proxy} is required" && exit 1)

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /app ./cmd/${BINARY}

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app /app
ENTRYPOINT ["/app"]
