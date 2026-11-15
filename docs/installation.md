# Installation

From an empty cluster to an agent calling its first tool through the gateway.

The examples use the OAuth provider at `id.example.com`, the namespace
`mcp-system` for mcp-gateway, and two MCP backends, `source-intelligence` and
`artifact`. Replace them with yours.

**Contents**

- [What you need](#what-you-need)
- [1. An OAuth client per agent](#1-an-oauth-client-per-agent)
- [2. Install the chart](#2-install-the-chart)
- [3. Register backends, select tools, grant them](#3-register-backends-select-tools-grant-them)
- [4. Connect an agent](#4-connect-an-agent)
- [5. Keep agents away from the backends](#5-keep-agents-away-from-the-backends)
- [6. The console](#6-the-console)
- [7. Verify](#7-verify)

## What you need

| | |
|---|---|
| Kubernetes | A cluster and permission to install cluster-scoped resources: four CRDs and two ClusterRoles. The integration tests run against Kubernetes 1.34. |
| Helm | 3.8 or later, for charts from an OCI registry. |
| An OAuth 2.0 provider | One that issues access tokens by the client credentials grant, and either publishes a JWKS for JWT access tokens or offers RFC 7662 introspection for opaque ones. Ory Hydra, Keycloak, Zitadel and others do. |
| MCP backends | Servers speaking MCP over Streamable HTTP, or over HTTP+SSE with `spec.transport: SSE`. Any version works towards backends; the SDK negotiates it. |
| Agents | Clients speaking **MCP 2026-07-28** or Streamable HTTP back to **2025-03-26** — directly with an OAuth token, or behind `mcp-gateway-proxy`, which adds the token and passes everything else unchanged. HTTP+SSE clients are refused; see [protocol compatibility](protocol-compatibility.md). |

## 1. An OAuth client per agent

Create a client in your provider for every agent, with the grant type
`client_credentials`. The gateway derives the **subject** from the token —
by default its `sub` claim, which for this grant is normally the client ID —
and `MCPGrant`s name that subject.

Keep the client IDs readable where you can (`coding-agent`), since grants name
them. If your provider generates IDs, see
[Authentication: the subject](authentication.md#when-the-provider-generates-client-ids).

## 2. Install the chart

A values file, with at least one way of verifying tokens:

```yaml
imagePullSecrets:
  - name: ghcr                 # see below
gateway:
  oauth:
    issuer: https://id.example.com/
    # The gateway's own resource identifier, as tokens for it carry it in aud.
    audience: https://mcp-gateway.example.com/mcp
    # JWT access tokens:
    jwksURL: https://id.example.com/.well-known/jwks.json
    # or opaque ones, by introspection:
    # introspection:
    #   url: http://id-admin.id.svc:4445/admin/oauth2/introspect
```

The chart refuses to render without `gateway.oauth.jwksURL` or
`gateway.oauth.introspection.url`: a gateway that cannot verify tokens cannot
serve anyone. Set `issuer` and `audience` too — without an audience, a token the
same provider issued for any other service is accepted.

The chart and the images are on ghcr.io, readable by members of the
metapatrol organisation for now. Log in with a GitHub token that has
`read:packages`, and give the cluster the same as the pull secret the values name:

```bash
echo "$GITHUB_TOKEN" | helm registry login ghcr.io -u <github-user> --password-stdin
kubectl create namespace mcp-system
kubectl -n mcp-system create secret docker-registry ghcr \
  --docker-server=ghcr.io --docker-username=<github-user> --docker-password="$GITHUB_TOKEN"
```

```bash
helm install mcp-gateway oci://ghcr.io/metapatrol/charts/mcp-gateway \
  --namespace mcp-system -f values.yaml
```

That installs the four CRDs, the controller and two gateway replicas. The
images come from `ghcr.io/metapatrol/`; the image tag defaults to the chart
version. Every value is listed in the [chart's README](../charts/mcp-gateway/README.md).

A healthy start:

```bash
kubectl -n mcp-system get pods
kubectl get mcp-gateway          # nothing yet: no backends are registered
```

## 3. Register backends, select tools, grant them

```yaml
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPServer
metadata:
  name: artifact
spec:
  endpoint: http://artifact.mcp-system.svc/mcp
---
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPToolSet
metadata:
  name: artifact-read
spec:
  serverRef: artifact
  tools: [artifact_search, artifact_get]
---
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPCapability
metadata:
  name: code-research
spec:
  toolSets: [artifact-read]
---
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPGrant
metadata:
  name: coding-agent
spec:
  subject: coding-agent        # the subject from part 1
  capabilities: [code-research]
```

The controller discovers the backend's catalog and reports what it found:

```bash
kubectl get mcp-gateway
kubectl get mcptoolsets -o wide     # which tools each tool set resolves to
```

A backend that wants credentials — an API key, an OAuth client of the
gateway's own, or the calling agent's token — says so in `spec.auth`:
[samples/mcpserver-auth.yaml](../samples/mcpserver-auth.yaml),
[CRD Reference](crd-reference.md#specauth). Its Secrets go into the namespace
the chart is installed in, and nowhere else.

More examples in [samples/](../samples), every field in the
[CRD Reference](crd-reference.md).

## 4. Connect an agent

The endpoint is `http://mcp-gateway.mcp-system.svc/mcp`, for an agent in the
cluster. Put an ingress in front of the `mcp-gateway` Service for agents
outside it.

**Direct.** An agent that does OAuth itself obtains a token with its client
credentials and sends it as `Authorization: Bearer <token>` on every request.
With `gateway.resourceURL` and `gateway.authorizationServers` set, every 401
tells an OAuth-capable client where to get one
([RFC 9728](authentication.md#protected-resource-metadata-rfc-9728)).

**Through the proxy.** An agent that cannot do OAuth talks to
`mcp-gateway-proxy` on loopback, without credentials, and the proxy does the
rest. As a sidecar: [samples/proxy-sidecar.yaml](../samples/proxy-sidecar.yaml).
On a workstation, with the binary from the
[releases page](https://github.com/metapatrol/mcp-gateway/releases):

```bash
MCP_GATEWAY_CLIENT_SECRET=... mcp-gateway-proxy \
  --gateway-url=https://mcp-gateway.example.com/mcp \
  --token-url=https://id.example.com/oauth2/token \
  --client-id=coding-agent
# the agent connects to http://127.0.0.1:8765/mcp
```

→ [Authentication: mcp-gateway-proxy](authentication.md#mcp-gateway-proxy)

## 5. Keep agents away from the backends

The gateway is the enforcement point only if agents cannot go around it. Allow
the backends' ingress from the gateway, and from the controller for discovery,
and from nothing else. A NetworkPolicy for that belongs in each backend's
namespace:
[Architecture: network model](architecture.md#network-model).

`networkPolicy.enabled` in the chart covers the gateway's own side: which
namespaces and pods may call it.

## 6. The console

Optional, and read-only:

```bash
helm upgrade mcp-gateway oci://ghcr.io/metapatrol/charts/mcp-gateway \
  --namespace mcp-system --reuse-values --set ui.enabled=true
kubectl -n mcp-system port-forward svc/mcp-gateway-ui 8080:80
```

It has no sign-in of its own; put a port-forward or an authenticating proxy in
front of it. → [Console](console.md)

## 7. Verify

1. **Status.** `kubectl get mcp-gateway` shows every server `Ready`, and every
   tool set, capability and grant `Valid` and `Ready`. Anything that is not
   says why in its `Reason` column (`-o wide`) and its conditions.
2. **What the subject gets.** The console's *Subjects* page, for
   `coding-agent`, lists every tool it is offered and the grant it comes
   through — and the tools it is not offered, and why.
3. **A call.** Have the agent call a granted tool. The gateway logs it as an
   audit record:

   ```bash
   kubectl -n mcp-system logs -l app.kubernetes.io/component=gateway | grep '"tool call"'
   ```

4. **A refusal.** Call a tool the agent holds no grant for. It gets the same
   error as for a tool that does not exist, and the gateway logs
   `tool call denied`.

Next: [Operations](operations.md) — metrics, the dashboard, what to do when
something is not Ready, upgrades.
