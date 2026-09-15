# MCP Gateway

## 1. Purpose

MCP Gateway is a Kubernetes-native control and data plane for exposing multiple Model Context Protocol (MCP) backends through a single governed access layer.

Its primary goals are:

- provide one MCP entry point for agents
- aggregate tools from multiple MCP backends
- expose only approved subsets of tools
- compose reusable capabilities from those tool subsets
- bind those capabilities to authenticated subjects
- centralize authentication, authorization, routing, and backend credential handling
- remain compatible with agents that either support OAuth natively or require a local auth-less MCP endpoint
- keep backend discovery dynamic instead of duplicating tool definitions into Kubernetes resources

MCP Gateway is not just an MCP reverse proxy. It is intended to act as a Kubernetes-native MCP access and capability layer.

---

## 2. Core Architecture

MCP Gateway consists of three runtime components:

- `mcp-gateway-controller`
- `mcp-gateway`
- `mcp-gateway-proxy`

### mcp-gateway-controller

The controller is the Kubernetes control-plane component.

Responsibilities:

- reconcile MCP Gateway CRDs
- discover tools exposed by registered MCP servers
- verify declared tool selections
- maintain resource status
- detect backend catalog changes
- trigger downstream configuration refreshes where required
- report invalid, unavailable, or degraded resources

The controller must not become part of the request path between an agent and an MCP backend.

### mcp-gateway

The gateway is the shared data-plane component.

Responsibilities:

- expose a single MCP endpoint
- authenticate incoming callers
- resolve the authenticated subject
- determine allowed capabilities
- determine allowed tool sets
- aggregate `tools/list`
- authorize every tool call
- resolve the target MCP backend
- inject backend-specific credentials where required
- forward MCP calls
- return backend responses
- emit audit and observability data

The gateway is the authoritative runtime enforcement point.

### mcp-gateway-proxy

The proxy is an optional lightweight sidecar or local adapter.

It exists only for agents that should not or cannot handle OAuth configuration themselves.

Responsibilities:

- expose an auth-less MCP endpoint to the local agent
- obtain OAuth access tokens via Client Credentials
- cache and refresh short-lived access tokens
- inject the bearer token when forwarding requests to `mcp-gateway`

The proxy must not contain authorization policy or backend-specific credentials.

Its purpose is convenience, not security enforcement.

---

## 3. Supported Access Modes

MCP Gateway supports two equivalent client modes.

### Direct Mode

```text
Agent
  |
  | OAuth Bearer Token
  v
mcp-gateway
  |
  v
MCP Backend
```

The agent performs OAuth Client Credentials itself.

### Proxy Mode

```text
Agent
  |
  | auth-less localhost MCP
  v
mcp-gateway-proxy
  |
  | OAuth Bearer Token
  v
mcp-gateway
  |
  v
MCP Backend
```

The gateway must behave identically in both modes.

It must not depend on whether the caller is an agent directly or an `mcp-gateway-proxy`.

---

## 4. Authentication

The first implementation uses OAuth 2.0 Client Credentials only.

API keys are explicitly out of scope for the initial version.

The intended flow is:

```text
client_id + client_secret
        |
        v
OAuth token endpoint
        |
        v
short-lived access token
        |
        v
mcp-gateway
```

The OAuth provider is an implementation detail.

ORY Hydra is a likely candidate, but MCP Gateway must not depend on Hydra-specific behavior.

The gateway validates the access token and derives a canonical `subject`.

The exact claim mapping is implementation-specific, but MCP Gateway should use one stable subject value for authorization.

Conceptually:

```text
OAuth credential
    ->
authenticated subject
    ->
MCPGrant
```

Authentication and authorization must remain separate.

---

## 5. Network Model

The shared gateway forms the network boundary between agents and MCP backends.

Agents must not require direct access to MCP backends.

Preferred network path:

```text
Agent
  |
  v
mcp-gateway
  |
  +--> source-intelligence MCP
  |
  +--> artifact MCP
  |
  +--> engram MCP
  |
  +--> cortex MCP
  |
  +--> future MCP backends
```

Kubernetes NetworkPolicies should allow:

```text
Agent -> Gateway
Gateway -> MCP Backends
```

and deny:

```text
Agent -> MCP Backends
```

This enables the gateway to act as the central enforcement and credential-brokering layer.

The optional sidecar proxy must not be treated as the security boundary.

---

# 6. Kubernetes API Model

The initial API consists of four CRDs:

```text
MCPServer
MCPToolSet
MCPCapability
MCPGrant
```

The relationship is:

```text
MCPServer
   |
   v
MCPToolSet
   |
   v
MCPCapability
   |
   v
MCPGrant
   |
   v
Subject
```

Each resource represents a separate concern.

---

## 7. MCPServer

`MCPServer` registers an MCP backend.

It describes where a backend exists and how MCP Gateway can reach it.

It must not contain a manually copied list of tools.

Example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPServer
metadata:
  name: source-intelligence
spec:
  endpoint: http://source-intelligence.mcp-system.svc/mcp
```

Another example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPServer
metadata:
  name: artifact
spec:
  endpoint: http://artifact.mcp-system.svc/mcp
```

The exact authentication configuration toward the backend can be added later as required.

### Design Rule

`MCPServer.spec` describes desired connection configuration.

The tool catalog is discovered dynamically from the backend.

---

## 8. MCP Discovery

The controller discovers the current tool catalog of an `MCPServer` through MCP itself.

Conceptually:

```text
MCPServer
    |
    v
initialize
    |
    v
tools/list
    |
    v
runtime catalog
```

The discovered catalog should not become a second manually maintained source of truth.

The controller may cache information such as:

- tool name
- description
- input schema
- catalog revision
- last successful discovery time

The Kubernetes API should not be used as a large cache for complete tool schemas unless there is a strong reason.

A compact `MCPServer.status` is preferable.

Example:

```yaml
status:
  available: true
  discoveredTools: 17
  catalogRevision: sha256:...
  lastDiscoveryTime: ...
```

---

# 9. MCPToolSet

`MCPToolSet` selects a curated subset of tools from exactly one `MCPServer`.

Its purpose is to create reusable technical toolboxes.

Example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPToolSet
metadata:
  name: source-intelligence-analysis
spec:
  serverRef: source-intelligence
  tools:
    - source_search
    - source_get
    - symbol_search
```

Another example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPToolSet
metadata:
  name: artifact-readwrite
spec:
  serverRef: artifact
  tools:
    - artifact_search
    - artifact_get
    - artifact_publish
```

### Design Rule

An `MCPToolSet` references exactly one `MCPServer`.

Cross-server composition belongs to `MCPCapability`.

---

## 10. MCPToolSet Verification

The controller verifies each `MCPToolSet` against the dynamically discovered catalog of the referenced `MCPServer`.

Verification includes at least:

- referenced `MCPServer` exists
- backend is reachable
- declared tools exist
- discovered catalog is sufficiently current

Example status:

```yaml
status:
  conditions:
    - type: Valid
      status: "True"

    - type: Ready
      status: "False"
      reason: MissingTools
      message: "symbol_search is not currently exposed by source-intelligence"
```

Suggested semantics:

```text
Valid
  declarative configuration is structurally valid

Ready
  referenced backend and tools are currently available
```

Examples:

```text
invalid CRD configuration
  -> Valid=False

backend unavailable
  -> Valid=True, Ready=False

all selected tools available
  -> Valid=True, Ready=True
```

The gateway must still perform runtime authorization and routing checks.

Controller verification is not a substitute for runtime enforcement.

---

# 11. MCPCapability

`MCPCapability` combines one or more `MCPToolSet` resources into a useful higher-level ability.

Unlike `MCPToolSet`, a capability may span multiple MCP backends.

Example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPCapability
metadata:
  name: code-research
spec:
  toolSets:
    - source-intelligence-analysis
    - artifact-read
```

Another example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPCapability
metadata:
  name: implementation
spec:
  toolSets:
    - source-intelligence-analysis
    - artifact-readwrite
    - cortex-development
```

Capabilities should represent useful agent abilities, not low-level backend structure.

Examples:

```text
code-research
code-review
artifact-authoring
memory
architecture-analysis
implementation
cluster-observation
```

---

# 12. MCPGrant

`MCPGrant` assigns one or more capabilities to an authenticated subject.

Example:

```yaml
apiVersion: mcp-gateway.example.io/v1alpha1
kind: MCPGrant
metadata:
  name: coding-agent
spec:
  subject: coding-agent
  capabilities:
    - code-research
    - artifact-authoring
```

The gateway resolves access as:

```text
authenticated subject
       |
       v
MCPGrant
       |
       v
MCPCapability
       |
       v
MCPToolSet
       |
       v
MCPServer
       |
       v
Tool
```

The `subject` is intentionally opaque to the Kubernetes API.

The CRD must not expose OAuth implementation details such as:

```text
subject.oauthClient.clientId
```

Instead:

```yaml
spec:
  subject: coding-agent
```

The gateway maps the authenticated OAuth identity to this subject.

---

# 13. Tool Aggregation

The gateway exposes one aggregated MCP tool catalog to the caller.

Example backend state:

```text
source-intelligence:
  source_search
  source_get
  symbol_search
  repository_search

artifact:
  artifact_search
  artifact_get
  artifact_publish
```

For a subject with only:

```text
source-intelligence-analysis
artifact-read
```

the gateway may expose:

```text
source_search
source_get
symbol_search
artifact_search
artifact_get
```

The caller must not see tools outside its grants.

---

## 14. Tool Identity and Name Collisions

Internally, MCP Gateway should maintain stable backend-qualified identities.

Example:

```text
source-intelligence/source_search
artifact/artifact_search
```

Externally, the gateway may expose cleaner tool names.

Automatic backend-name prefixing should not be mandatory.

If collisions occur, aliases may later be introduced.

Example concept:

```yaml
tools:
  - source: search
    exposeAs: memory_search
```

Alias support is optional for the first implementation.

---

# 15. Runtime Request Flow

## tools/list

```text
Agent
  |
  v
Gateway
  |
  +--> authenticate subject
  |
  +--> resolve MCPGrant
  |
  +--> resolve MCPCapabilities
  |
  +--> resolve MCPToolSets
  |
  +--> load current discovered catalogs
  |
  +--> filter tools
  |
  +--> aggregate result
  |
  v
Agent
```

The gateway may use cached discovered catalogs.

A slow or unavailable MCP backend should not unnecessarily block unrelated backends.

Caching behavior and degradation policy are implementation details to be defined later.

---

## tools/call

```text
Agent
  |
  v
Gateway
  |
  +--> authenticate
  |
  +--> resolve subject
  |
  +--> authorize requested tool
  |
  +--> resolve MCP backend
  |
  +--> obtain/inject backend credential
  |
  +--> forward MCP request
  |
  v
MCP Backend
```

Authorization must be evaluated on every call.

The gateway must not assume that visibility in a previous `tools/list` response permanently authorizes a later invocation.

---

# 16. Backend Credential Handling

Agent credentials and backend credentials are separate concerns.

The intended model is:

```text
Agent OAuth identity
       |
       v
MCP Gateway authorization
       |
       v
backend-specific credential
       |
       v
MCP backend
```

The agent's bearer token should not automatically be forwarded to arbitrary MCP backends.

The gateway may later support:

- static backend credentials
- OAuth Client Credentials
- token exchange
- mTLS
- workload identity
- other backend-specific authentication mechanisms

These are implementation details behind `MCPServer` or related future configuration.

---

# 17. Change Detection

If an MCP backend changes its tool catalog, the controller should detect the change.

Conceptual flow:

```text
MCP backend catalog changes
       |
       v
Discovery
       |
       v
catalog revision changes
       |
       v
affected MCPToolSets reverified
       |
       v
affected capabilities/grants recalculated
       |
       v
gateway catalog refreshed
```

Where supported by the MCP transport/version in use, connected clients may be informed that the visible tool list changed.

The exact notification mechanism should follow the MCP specification version selected during implementation.

---

# 18. Failure Behavior

MCP Gateway should avoid turning one broken backend into a global outage.

Examples:

### Backend unavailable

```text
source-intelligence unavailable
artifact available
```

The gateway should still be able to expose artifact tools where possible.

Affected `MCPToolSet` resources become `Ready=False`.

### Missing tool

If a configured tool disappears:

```text
MCPToolSet -> Ready=False
```

The missing tool must no longer be exposed.

Unrelated tools may remain available.

### Invalid grant

A grant referencing a missing capability must report invalid status and must not accidentally broaden access.

Fail closed.

---

# 19. Non-Goals for the Initial Version

The initial version should avoid unnecessary complexity.

Explicitly out of scope unless later required:

- API key authentication
- complex policy languages
- attribute-based access control
- per-tool conditional rules
- inheritance between grants
- grant overrides
- backend tool definition persistence as CRD spec
- per-agent gateway deployments
- mandatory sidecars
- dynamic code/plugin execution inside the gateway
- custom MCP implementation per backend
- OAuth-provider-specific logic

The first version should remain small and deterministic.

---

# 20. Initial MCP Backends

The architecture must support arbitrary MCP backends.

Initial and expected examples include:

```text
source-intelligence
artifact
```

Expected future backends include:

```text
engram
cortex
...
```

Adding a backend should require registration and policy configuration, not gateway source-code changes.

---

# 21. Project Naming

Project:

```text
MCP Gateway
```

Binaries:

```text
mcp-gateway
mcp-gateway-controller
mcp-gateway-proxy
```

Roles:

```text
mcp-gateway
  shared MCP data plane and enforcement point

mcp-gateway-controller
  Kubernetes control plane

mcp-gateway-proxy
  optional local OAuth adapter / sidecar
```

The gateway is what agents talk to and what the project is named after, so it
takes the plain name; the other two carry their role as a suffix.

API group:

```text
mcp-gateway.metapatrol.com
```

CRDs:

```text
MCPServer
MCPToolSet
MCPCapability
MCPGrant
```

---

# 22. Architectural Principles

The implementation should preserve the following principles:

1. **One source of truth**
   Tool definitions come from MCP backends themselves.

2. **Discovery over duplication**
   MCP Gateway discovers tool catalogs instead of copying them manually into CRDs.

3. **Authentication is not authorization**
   OAuth authenticates a subject. `MCPGrant` authorizes that subject.

4. **The gateway is the enforcement point**
   Controller-side validation improves correctness but never replaces runtime authorization.

5. **The proxy is optional**
   It exists only to reduce client configuration complexity.

6. **The proxy is not a security boundary**
   Network and authorization enforcement belong to the shared gateway.

7. **Capabilities are backend-independent**
   `MCPCapability` expresses useful agent abilities and may combine multiple MCP servers.

8. **Tool sets remain backend-local**
   One `MCPToolSet` selects tools from exactly one `MCPServer`.

9. **Fail closed**
   Invalid or unresolved authorization must never result in broader access.

10. **Backends remain autonomous**
    MCP Gateway routes to existing MCP servers rather than reimplementing their business logic.

11. **Kubernetes remains the desired-state API**
    Runtime catalogs and large schemas should not unnecessarily become persistent Kubernetes objects.

12. **Implementation technology is intentionally undecided**
    Language, frameworks, persistence, cache technology, OAuth provider integration, and concrete MCP libraries are to be selected separately.

---

# 23. Minimal Initial Milestone

A useful first milestone should implement only:

```text
MCPServer
MCPToolSet
MCPCapability
MCPGrant
```

plus:

```text
mcp-gateway-controller
mcp-gateway
```

The first milestone should support:

- registration of MCP servers
- dynamic tool discovery
- tool-set verification
- OAuth bearer-token validation
- subject resolution
- grant resolution
- filtered aggregated `tools/list`
- authorized `tools/call`
- routing to multiple MCP backends
- basic status conditions
- basic structured logging and metrics

`mcp-gateway-proxy` can be implemented immediately afterward because it is largely independent of the core authorization model.

The first two real integration targets should be:

```text
source-intelligence MCP
artifact MCP
```

This provides enough variety to validate cross-backend capabilities without introducing unnecessary complexity.