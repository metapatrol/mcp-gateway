# Development

**Contents**

- [Layout](#layout)
- [Building and testing](#building-and-testing)
- [Running it locally](#running-it-locally)
- [Changing things](#changing-things)
- [Performance](#performance)
- [Releases](#releases)

## Layout

```text
cmd/mcp-gateway             main of the data plane
cmd/mcp-gateway-controller  main of the control plane
cmd/mcp-gateway-proxy       main of the local OAuth adapter
cmd/mcp-gateway-ui          main of the console
api/v1alpha1                the four kinds, their conditions and reasons
api/openapi                 the inspect API's spec, embedded and served
internal/policy             resolution: grant → capability → tool set → backend
                            tool; pure, shared by the controller and the gateway
internal/discovery          the MCP client side: one connection per backend, its
                            listener, catalogs, forwarding
internal/credentials        spec.auth: Secrets, OAuth clients, API keys, passthrough
internal/auth               token verification: JWT against a JWKS, introspection
internal/oauth              the client credentials grant, for the proxy
internal/gateway            the data plane: HTTP handler, per-subject servers,
                            authorisation, audit, the gateway's reconcilers
internal/controller         the control plane: verification and status
internal/inspect            the inspect API and its event stream;
internal/inspect/api          generated from api/openapi/inspect.yaml
internal/proxy              mcp-gateway-proxy
internal/ui                 mcp-gateway-ui: the embedded web UI, /api passed on
internal/metrics            Prometheus metrics
internal/tracing            OpenTelemetry setup, OTLP/HTTP
internal/mcptest            real MCP backends for tests
web/ui                      the console's web UI (Angular)
charts/mcp-gateway          the Helm chart; its crds/ are generated
charts/mcp-gateway-grafana-dashboard
samples                     example resources, checked by the integration tests
tests/integration           controller and gateway against a real API server
tests/load                  throughput and CPU per call
```

The controller and the gateway resolve through the same `internal/policy`
code, so the status a resource shows describes what the gateway enforces.

## Building and testing

```bash
make generate               # deepcopy
make generate-crds          # CRD manifests → charts/mcp-gateway/crds/
make generate-api           # inspect API types and server interface ← api/openapi/inspect.yaml
make build                  # compile everything
make build-binaries         # bin/mcp-gateway{,-controller,-proxy,-ui}, the UI built first
make test                   # unit tests, with real in-process MCP backends
make integration-test       # controller + gateway against a real API server (envtest)
make lint                   # golangci-lint
make lint-api               # Redocly over the inspect API's spec
make ui-build ui-test ui-lint   # the console's web UI (Node ≥ 22.22.3)
make helm-lint              # lint both charts
make helm-docs              # regenerate the chart READMEs
make secret-scan            # gitleaks over the working tree
```

The tests talk real MCP: the backends in [`internal/mcptest`](../internal/mcptest)
are the SDK's own server behind a local listener — session-based and MCP
2026-07-28 — so notifications and reconnects are exercised as they happen in
production. The integration tests download kube-apiserver and etcd through
`setup-envtest` once, at the versions the Makefile pins, and send every
resource in [`samples/`](../samples) to the API server as a dry run: samples
are documentation people copy.

### The console's packages

The console's `@metapatrol/*` packages — the shell, components and styles every
metapatrol console shares — come from a private npm registry
(`web/ui/.npmrc`). Inside the metapatrol network that is the forge's: export a
Forgejo token with `read:package` scope as `NPM_TOKEN`, and point
`NODE_EXTRA_CA_CERTS` at the platform CA where the system does not trust it.

Outside it, the same versions are on GitHub Packages, which wants a GitHub
token with `read:packages` even for public packages. Point `web/ui/.npmrc` at
it and drop the forge's tarball URLs and checksums of the `@metapatrol`
entries from `package-lock.json` — the tarballs on GitHub are built there —
the way the step *Point the console at GitHub Packages* in
[`publish.yml`](../.github/workflows/publish.yml) does. Then `make ui-install`
with the token as `NPM_TOKEN`. Keep both changes out of your commits.

Everything but the console builds and tests without them.

## Running it locally

```bash
make dev-stack
```

starts everything on this machine until Ctrl-C: an API server with the CRDs,
MCP backends with example resources, the controller, the gateway with its
inspect API, and the console at http://127.0.0.1:8480/. No cluster needed.
→ [Console: developing it](console.md#developing-it)

To try an unreleased image on a cluster, build the one you need:

```bash
docker build --build-arg BINARY=mcp-gateway -t mcp-gateway:dev .
```

`BINARY` picks the binary: `mcp-gateway`, `mcp-gateway-controller`,
`mcp-gateway-proxy` or `mcp-gateway-ui`.

## Changing things

- **The four kinds.** Change `api/v1alpha1`, then `make generate
  generate-crds`, and describe the field in the inspect API's spec as well —
  a test fails when a field of the four kinds is missing from it.
- **The inspect API** is written spec first: change
  [`api/openapi/inspect.yaml`](../api/openapi/inspect.yaml), then
  `make generate-api`. A test fails while the generated Go code is stale, and
  the UI generates its client from the spec on every build, so a change the UI
  does not follow fails its build.
- **A new condition reason** goes into `api/v1alpha1` and into the
  [CRD Reference](crd-reference.md). Reasons are stable: alerts match on them.
- **A new metric** goes into `internal/metrics`, the table in
  [Operations](operations.md#metrics), and a panel of the dashboard.
- **A new variant of `spec.auth`** is another member of the union and another
  `credentials.Credential`.
- **Anything a backend sends** — tool definitions, instructions, errors — is
  data. It never decides where the gateway connects or what it forwards.

## Performance

Measured rather than guessed:

```bash
go test -run '^$' -bench . ./internal/policy ./internal/auth     # per-call policy and token work
MCP_GATEWAY_LOAD=1 go test ./tests/load -run TestLoad -v -timeout 30m
```

The load test runs MCP clients, the gateway and backends in one process over
loopback HTTP and reports requests per second, latency percentiles and CPU per
call, straight to a backend and through the gateway. It needs a quiet machine:
on a busy one, differences of ten percent are noise.

## Releases

Work lands on `develop`. Merging `develop` into `main` runs the checks again,
builds the images from the commit, and runs semantic-release, which decides the
version from the conventional commits since the last tag, writes the
changelog and tags the release. The release then builds the tagged images, the
binaries and both charts in the maintainers' Forgejo —
[.forgejo/README.md](../.forgejo/README.md).

The tag reaches GitHub through the push mirror, where
[`publish.yml`](../.github/workflows/publish.yml) builds the same sources into
the images and charts on ghcr.io and the GitHub release.
