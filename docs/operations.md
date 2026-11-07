# Operations

Running mcp-gateway: health, metrics, audit records, what to do when something
is not Ready, scaling and upgrades.

**Contents**

- [Health](#health)
- [Metrics](#metrics)
- [The Grafana dashboard](#the-grafana-dashboard)
- [Alerts](#alerts)
- [Audit records](#audit-records)
- [Traces](#traces)
- [When something is not Ready](#when-something-is-not-ready)
- [Scaling](#scaling)
- [Upgrades](#upgrades)

## Health

Both components serve `/healthz` and `/readyz` on `:8081`.

| Component | Ready when |
|---|---|
| `mcp-gateway` | it has read the four kinds and built its first policy snapshot. Before that it serves no traffic. |
| `mcp-gateway-controller` | its manager is running. |

The gateway does not depend on the controller: a controller that is down
changes nothing for agents, and only lets status go stale.

## Metrics

Both components serve Prometheus metrics on `:8080`, and the chart creates a
metrics Service for each; `serviceMonitor.enabled` renders a ServiceMonitor
(it needs the Prometheus operator's CRDs, and most Prometheus installations
select ServiceMonitors by a label — `serviceMonitor.additionalLabels`).

| Metric | Component | What it answers |
|---|---|---|
| `mcp_gateway_resources{kind,ready}` | controller | How many of each kind are Ready. `False` and `Unknown` report zero rather than disappearing |
| `mcp_gateway_discoveries_total{server,outcome}` | both | Catalog reads against each backend |
| `mcp_gateway_backend_available{server}` | both | Whether the last discovery of a backend succeeded |
| `mcp_gateway_discovered_tools{server}` | both | Catalog size per backend |
| `mcp_gateway_tool_calls_total{server,tool,outcome}` | gateway | Forwarded calls: `ok`, `tool_error`, `backend_error`, `unauthorized` |
| `mcp_gateway_tool_call_duration_seconds{server,tool}` | gateway | How long backends take |
| `mcp_gateway_subject_tool_calls_total{subject,outcome}` | gateway | Forwarded calls by the subject that made them |
| `mcp_gateway_tool_calls_denied_total{subject}` | gateway | Calls refused for want of a grant |
| `mcp_gateway_auth_failures_total{reason}` | gateway | Refused tokens, by reason: `missing_token`, `expired`, `audience`, `signature`, `unavailable`, … |
| `mcp_gateway_inspect_requests_total{operation,code}` | gateway | Inspect API requests, by the spec's operationId (`none` if no operation was reached) |
| `mcp_gateway_inspect_request_duration_seconds{operation}` | gateway | Inspect API latency, the event stream excepted |
| `mcp_gateway_inspect_event_streams` | gateway | Open event streams |
| `mcp_gateway_inspect_auth_failures_total{reason}` | gateway | Refused inspect tokens: `missing_token`, `review_failed`, `not_authenticated`, `wrong_audience`, `not_service_account`, `service_account_not_allowed` |
| `mcp_gateway_build_info{component,…}` | all | Version, commit, build time, Go version |

controller-runtime's own `controller_runtime_*` and `workqueue_*` families come
with them.

## The Grafana dashboard

A second chart ships a dashboard built on these metrics, as a ConfigMap for
the Grafana sidecar:

```bash
helm install mcp-gateway-dashboard oci://ghcr.io/metapatrol/charts/mcp-gateway-grafana-dashboard \
  --namespace monitoring
```

Its rows: tool calls, access, backend availability, resource state, the
inspect API, reconcilers and runtime. → [Dashboard chart](../charts/mcp-gateway-grafana-dashboard/README.md)

## Alerts

Starting points, to be tuned to your traffic:

```yaml
- alert: MCPBackendUnavailable
  expr: max by (server) (mcp_gateway_backend_available) == 0
  for: 5m
  annotations:
    summary: "MCP backend {{ $labels.server }} cannot be discovered; its tools drop out of tool lists"

- alert: MCPResourcesNotReady
  expr: sum by (kind) (mcp_gateway_resources{ready!="True"}) > 0
  for: 15m
  annotations:
    summary: "{{ $value }} {{ $labels.kind }} not Ready — kubectl get mcp-gateway -o wide"

- alert: MCPToolCallsFailing
  expr: |
    sum by (server) (rate(mcp_gateway_tool_calls_total{outcome=~"backend_error|unauthorized"}[10m]))
      / sum by (server) (rate(mcp_gateway_tool_calls_total[10m])) > 0.1
  for: 10m

- alert: MCPTokenVerificationUnavailable
  expr: sum(rate(mcp_gateway_auth_failures_total{reason="unavailable"}[5m])) > 0
  for: 5m
  annotations:
    summary: "The gateway cannot reach the OAuth provider's JWKS or introspection endpoint"
```

A rise in `mcp_gateway_tool_calls_denied_total` is usually a grant that was
changed or not applied yet, and sometimes an agent trying tools it was never
given.

## Audit records

The gateway logs every tool call as an audit record, and every refusal, with
the fields `subject`, `clientID` (when the token has one), `tool`, `server`
(once resolved) and `duration` (once forwarded):

| `msg` | Means |
|---|---|
| `tool call` | Forwarded, and the backend answered |
| `tool call returned an error` | Forwarded; the tool reported an error (`isError`) |
| `tool call denied` | Not forwarded: no grant, or the backend does not expose the tool |
| `tool call failed` | Not forwarded, or failed on the way: backend unavailable, credentials unavailable |
| `tool call refused by backend` | The backend answered 401 or 403 |
| `tool call rejected by backend` | The backend answered with a JSON-RPC error |

Logs are JSON by default (`logging.format`). The arguments and results of
tool calls are not logged.

## Traces

With an OTLP endpoint set, the gateway exports a span for every `tools/call`
over OTLP/HTTP:

```yaml
gateway:
  tracing:
    endpoint: http://otel-collector.observability:4318
```

The value becomes `OTEL_EXPORTER_OTLP_ENDPOINT`, and `serviceName` becomes
`OTEL_SERVICE_NAME`. Any other standard `OTEL_*` variable, such as
`OTEL_EXPORTER_OTLP_HEADERS` or `OTEL_TRACES_SAMPLER`, goes into
`gateway.env`. Without an endpoint, nothing is exported.

A call that arrives with a W3C `traceparent` header continues that trace, and
the request to the backend carries the gateway's span on as its parent. A
harness that traces its model calls thus sees its tool calls, and a backend
that traces sees them as well, in one trace.

The span is named `tools/call <tool>`, its kind is server, and it carries:

| Attribute | Value |
|---|---|
| `mcp.method.name` | `tools/call` |
| `gen_ai.operation.name` | `execute_tool` |
| `gen_ai.tool.name` | The tool name the agent called |
| `user.id` | The subject |
| `client.id` | The token's `client_id`, when it has one |
| `mcp.server`, `mcp.tool` | The backend, and the tool's name there |
| `mcp.outcome` | `ok`, `tool_error`, `backend_error`, `unauthorized` (as in the metrics), or `denied` |

A denied call has a span too. It carries the name the agent sent and nothing
about the backend. `backend_error` and `unauthorized` mark the span as an
error. Catalog discoveries are not traced, and arguments and results never go
into a span.

## When something is not Ready

```bash
kubectl get mcp-gateway -o wide
kubectl describe mcptoolset <name>
```

Every condition has a stable reason, listed in the
[CRD Reference](crd-reference.md). The ones that come up most:

| Kind | Reason | Look at |
|---|---|---|
| MCPServer | `DiscoveryFailed` | The endpoint, and whether the controller may reach it — a NetworkPolicy in the backend's namespace that admits only the gateway also keeps the controller out. |
| MCPServer | `CredentialsUnavailable` | The message names the Secret and key, and lists the keys the Secret has. Secrets are read from the chart's namespace only. |
| MCPServer | `Unauthorized` | The backend refused the credentials `spec.auth` sends, or wants some and gets none. |
| MCPToolSet | `MissingTools` | A named tool the backend does not offer — renamed or removed on the backend's side, or a typo. |
| MCPToolSet | `UnmatchedPatterns` | A pattern that selects nothing; almost always a typo. |
| MCPToolSet | `ServerUnavailable` | The server; the tool set only passes it on. |
| MCPCapability, MCPGrant | `ToolNameConflict` | Two backends offer a tool under one name. Neither is exposed; rename one with a tool set's `prefix` or `aliases`. |
| MCPGrant | `CapabilityNotFound` | A capability name that does not exist. The rest of the grant still applies. |

The console's overview shows the same, the resource at fault first and the
ones only passing it on after.

What a replica of the gateway actually serves can differ from status for up to
a discovery interval: both discover on their own. The console says which
replica answered.

## Scaling

**The gateway** scales horizontally. Agents speak MCP 2026-07-28, which has no
sessions: any replica answers any request, and no ingress affinity is needed.
The chart runs two replicas with a PodDisruptionBudget. Each replica discovers
the backends itself and keeps its own connections to them.

**The controller** does its work in one replica. More replicas make sense only
with `controller.leaderElect: true`, and then one of them is active.

Per call, the gateway spends well under a millisecond of CPU on
authentication, authorisation and forwarding; a signature check is the largest
part, and a verified token is remembered until it expires.
[Development](development.md#performance) describes how that is measured.

## Upgrades

```bash
helm upgrade mcp-gateway oci://ghcr.io/metapatrol/charts/mcp-gateway \
  --namespace mcp-system --reuse-values --version <new version>
```

**Helm does not upgrade CRDs** it installed from a chart's `crds/` directory.
When a release changes them — the [changelog](../CHANGELOG.md) says so — apply
them from the release's tag first:

```bash
v=<new version>
for kind in mcpservers mcptoolsets mcpcapabilities mcpgrants; do
  kubectl apply --server-side -f "https://raw.githubusercontent.com/metapatrol/mcp-gateway/v$v/charts/mcp-gateway/crds/mcp-gateway.metapatrol.com_$kind.yaml"
done
```

or from a checkout: `kubectl apply --server-side -f charts/mcp-gateway/crds/`.

The API is `v1alpha1`. Changes that break manifests are marked as breaking in
the changelog.
