# CRD Reference

Group `mcp-gateway.metapatrol.com`, version `v1alpha1`. All four kinds are
cluster-scoped and share the category `mcp-gateway`:

```bash
kubectl get mcp-gateway
kubectl explain mcptoolset.spec
```

Every kind reports `status.observedGeneration` and a `Ready` condition; all but
`MCPServer` also report `Valid`. Their reasons are listed per kind below and are stable: alerts and
tooling may match on them.

---

## MCPServer

Registers an MCP backend. Short name `mcpsrv`.

```yaml
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPServer
metadata:
  name: source-intelligence
spec:
  endpoint: http://source-intelligence.mcp-system.svc/mcp
  discoveryInterval: 1m
```

### spec

| Field | Type | Required | Description |
|---|---|---|---|
| `endpoint` | string | yes | URL of the backend's MCP endpoint; with `transport: SSE`, the URL its event stream is opened at. Must start with `http://` or `https://`. |
| `transport` | string | no | `StreamableHTTP` (default) or `SSE`, for a backend that offers only the HTTP+SSE transport of MCP 2024-11-05. |
| `discoveryInterval` | duration | no | How often the catalog is re-read when the backend does not announce changes. Default `1m`; values below `10s` are raised to `10s`. |
| `auth` | object | no | How the gateway and the controller authenticate to the backend. Left out: no credentials. |

There is no list of tools. The catalog is discovered.

#### An SSE-only backend

Some servers still speak only the HTTP+SSE transport, at a URL such as
`/mcp/sse`. `transport: SSE` reaches them directly, without a bridge in front:

```yaml
spec:
  endpoint: http://crawl4ai.ai-search.svc/mcp/sse
  transport: SSE
  auth:
    type: APIKey
    apiKey:
      secretRef: {name: crawl4ai, key: token}
```

Agents still speak Streamable HTTP to the gateway. Each connection the gateway
holds to such a backend keeps an event stream open, since that stream is where
the backend's answers arrive; credentials go on the stream and on every request
posted. Changing the transport counts as changing the endpoint: the catalog is
discarded and read anew. WebSocket is not supported.

### spec.auth

A discriminated union: `type` says which of the other fields applies, and only
that one may be set. The API server refuses anything else.

| `type` | Catalog reads (controller, gateway) | Tool calls (gateway) |
|---|---|---|
| `None` (default) | no credentials | no credentials |
| `Passthrough` | `passthrough.discovery`, or no credentials | the calling agent's own token |
| `ClientCredentials` | a token for the gateway's own OAuth client | the same |
| `APIKey` | a static key from a Secret | the same |

```yaml
# An API key in a header of the backend's choosing.
auth:
  type: APIKey
  apiKey:
    secretRef: {name: memory-credentials, key: API_KEY}
    header: X-API-Key
```

```yaml
# The gateway's own OAuth client. hydra-maester writes CLIENT_ID and
# CLIENT_SECRET into one Secret; both are referenced by key.
auth:
  type: ClientCredentials
  clientCredentials:
    tokenURL: https://id.example.com/oauth2/token
    clientIDRef:     {name: memory-oauth, key: CLIENT_ID}
    clientSecretRef: {name: memory-oauth, key: CLIENT_SECRET}
    scopes: [memory.read]
    audience: [memory]
```

```yaml
# Each agent calls with its own token; the catalog is read with a key.
auth:
  type: Passthrough
  passthrough:
    discovery:
      apiKey:
        secretRef: {name: memory-discovery, key: token}
```

#### Secret references

Every secret value is a `{name, key}` pair. `key` names the data key explicitly,
so a Secret written by some other tool — `CLIENT_SECRET`, `client-secret`,
`token` — is used as it is.

There is no `namespace`. Secrets are read from the namespace the controller and
the gateway run in (`--secret-namespace`, set by the chart to the release
namespace). An `MCPServer` is cluster-scoped; a reference that could name any
namespace would let whoever may create one have the gateway read any Secret in
the cluster.

Values are trimmed of surrounding whitespace, since a key written from a file
usually ends in a newline. A Secret is read when a request needs it and kept for a
minute; a key the backend refuses is read again at once, so a rotation on both
ends is picked up on the first refusal.

#### passthrough

| Field | Type | Required | Description |
|---|---|---|---|
| `discovery.apiKey` | [apiKey](#apikey) | no | Authenticates catalog reads with a key. |
| `discovery.clientCredentials` | [clientCredentials](#clientcredentials) | no | Authenticates catalog reads as an OAuth client. |

`discovery`, when present, sets exactly one of the two. Left out, catalog reads
carry no credentials — right for a backend that lists its tools openly and guards
only the calls.

The agent's `Authorization` header goes to the backend exactly as the agent sent
it, and only on requests for that agent's tool calls. The gateway does not inspect
the token on the backend's behalf — no audience or scope check: which tokens the
backend accepts is the backend's to decide. Nothing is forwarded to a backend
whose `type` is not `Passthrough`.

A backend whose tool list depends on the caller is seen through the discovery
credential alone: every agent is offered what that credential can see, and the
backend decides on each call.

#### clientCredentials

| Field | Type | Required | Description |
|---|---|---|---|
| `tokenURL` | string | yes | The provider's token endpoint. |
| `clientID` | string | one of | The client ID, written out. |
| `clientIDRef` | [SecretKeyRef](#secret-references) | one of | The client ID from a Secret — for providers that generate it. |
| `clientSecretRef` | [SecretKeyRef](#secret-references) | yes | The client secret. |
| `scopes` | []string | no | Requested with each token. |
| `audience` | []string | no | Sent as the `audience` parameter (Ory Hydra), space-separated. |

The token is kept until 30 seconds before it expires, and replaced at once when
the backend answers 401 — the request is then sent once more with the new token.

#### apiKey

| Field | Type | Required | Description |
|---|---|---|---|
| `secretRef` | [SecretKeyRef](#secret-references) | yes | Where the key is. |
| `header` | string | no | The header it is sent in. Default `Authorization`. |
| `prefix` | string | no | Goes in front of the key. Left out: `Bearer ` with `Authorization`, nothing with any other header. `""` sends the key bare. |

### status

| Field | Description |
|---|---|
| `available` | Whether the most recent discovery succeeded. |
| `discoveredTools` | Number of tools in the last successful discovery. |
| `catalogRevision` | `sha256:…` over the tool definitions and the server's instructions of the last successful discovery. |
| `lastDiscoveryTime` | When discovery last succeeded. |

While the backend is down, `discoveredTools`, `catalogRevision` and
`lastDiscoveryTime` keep describing the last catalog that was actually seen.

| Condition | Reason | Meaning |
|---|---|---|
| `Ready=True` | `Discovered` | `tools/list` answered. |
| `Ready=False` | `DiscoveryFailed` | Unreachable, or not an MCP server. The message has the error. |
| `Ready=False` | `CredentialsUnavailable` | The credentials `spec.auth` names could not be obtained: a Secret or key is missing — the message names it, and lists the keys the Secret does have — or the token endpoint refused the client. |
| `Ready=False` | `Unauthorized` | The backend answered 401 or 403: it refused the credentials `spec.auth` sends, or wants credentials and `spec.auth` sends none. |

Printer columns: `Endpoint`, `Available`, `Tools`, `Ready`, `Age`; with
`-o wide` also `Transport`.

---

## MCPToolSet

A curated subset of the tools of **one** server, selected by name or by glob
pattern. Short name `mcpts`.

```yaml
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPToolSet
metadata:
  name: source-intelligence-read
spec:
  serverRef: source-intelligence
  tools:
    - code.*            # every tool whose name starts with "code."
    - repositories.*
    - refs.list         # an exact name
  exclude:
    - repositories.read_file
```

### spec

| Field | Type | Required | Description |
|---|---|---|---|
| `serverRef` | string | yes | Name of the `MCPServer`. |
| `tools` | []string | yes | Tool names or glob patterns. At least one; no duplicates. |
| `exclude` | []string | no | Tool names or glob patterns removed from the selection. Wins over `tools`. |
| `prefix` | string | no | Put in front of the name of every selected tool, as agents see it. Up to 64 letters, digits, `_`, `-`, `.`. |
| `aliases` | map[string]string | no | Backend tool name → the name agents see it under. Wins over `prefix`. Keys are exact names, values 1–128 letters, digits, `_`, `-`, `.`; at most 128 entries. |

**Patterns.** `*` matches any run of characters — dots included, and the empty
run — and `?` exactly one character. Matching is case-sensitive and anchored at
both ends: `code.*` matches `code.search`, not `xcode.search`; `*` matches every
tool. An entry without `*` or `?` is an exact name, so a tool set written with
names only keeps its meaning.

A pattern selects what the backend offers **now**. A tool the backend adds later
and a pattern matches is granted without a manifest changing — convenient for
`code.*`, and worth an `exclude` for anything destructive the backend might add:

```yaml
tools: ["artifact_*"]
exclude: ["artifact_delete"]
```

`status.resolvedTools` always shows what the tool set selects at the moment.

**Renaming.** Tools are offered under their backend names — unless the tool set
says otherwise. When two backends name a tool alike, a subject granted both gets
neither (`ToolNameConflict`); renaming one of them resolves that:

```yaml
spec:
  serverRef: engram
  tools: ["memory_*"]
  exclude: [memory_remember]
  prefix: engram_            # memory_list   -> engram_memory_list
  aliases:
    memory_search: recall    # memory_search -> recall (an alias wins over the prefix)
```

`tools`, `exclude` and the keys of `aliases` always use the backend's names; only
what agents see changes. A tool is offered under one name only — an aliased tool
is not also offered under its prefixed name. The gateway's instructions list a
renamed tool with its backend name beside it, `recall (memory_search)`, since
that is the name the backend's own instructions use.

### status

| Field | Description |
|---|---|
| `catalogRevision` | The server catalog the tools were last verified against. |
| `resolvedTools` | What the tool set selects from that catalog, sorted, under the names agents see: matched patterns and present names, minus exclusions, after `prefix` and `aliases`. What a grant on it hands out. |
| `backendNames` | For each resolved tool offered under another name, its backend name. |
| `resolvedToolCount` | Length of `resolvedTools`. |
| `missingTools` | Exact names in `tools` the server does not currently expose. |
| `unmatchedPatterns` | Patterns in `tools` that select no tool — none matches, or every match is excluded. |
| `unusedAliases` | Keys of `aliases` that name no tool the set selects. |
| `conflictingNames` | Names under which the set itself would offer more than one tool — two aliases alike, or an alias equal to another tool's prefixed name. None of those tools is offered. |

| Condition | Reason | Meaning |
|---|---|---|
| `Valid=True` | `Resolved` | The server exists. |
| `Valid=False` | `ServerNotFound` | `serverRef` names no `MCPServer`. |
| `Ready=True` | `Available` | The server is Ready, every name is exposed and every pattern selects something — `7 tools selected from source-intelligence`. |
| `Ready=False` | `Invalid` | The tool set is not valid. |
| `Ready=False` | `ServerUnavailable` | The server is not Ready. |
| `Ready=False` | `MissingTools` | Some names are not exposed — `"symbol_search" is not currently exposed by source-intelligence`. Takes precedence when patterns are unmatched too; the message lists both. |
| `Ready=False` | `ToolNameConflict` | The set's own renaming would offer two tools under one name — `"e_list" would name more than one tool, so neither is exposed`. Takes precedence over the reasons below. |
| `Ready=False` | `UnmatchedPatterns` | Some patterns select nothing — `"cod.*" selects no tool of source-intelligence`. A wildcard that matches nothing is almost always a typo. |
| `Ready=False` | `UnusedAliases` | Some aliases name a tool the set does not select — `"code.serach" has an alias but not selected`. |
| `Ready=Unknown` | `CatalogPending` | The controller has not read the server's catalog since it started. |

The gateway exposes what does resolve; a missing tool or an unmatched pattern
does not take the rest of the set with it.

Printer columns: `Server`, `Tools` (the resolved count), `Valid`, `Ready`, and
`Reason` and `Resolved` with `-o wide`.

---

## MCPCapability

A useful ability built from tool sets, possibly of several servers. Short name
`mcpcap`.

```yaml
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPCapability
metadata:
  name: code-research
spec:
  description: Read and search source code, and read published artifacts.
  toolSets:
    - source-intelligence-analysis
    - artifact-read
```

### spec

| Field | Type | Required | Description |
|---|---|---|---|
| `description` | string | no | What the capability is for. Shown by kubectl, never sent to agents. Up to 512 characters. |
| `toolSets` | []string | yes | Names of `MCPToolSet`s. At least one; no duplicates. |

### status

| Condition | Reason | Meaning |
|---|---|---|
| `Valid=True` | `Resolved` | Every tool set exists, and no name is claimed twice. |
| `Valid=False` | `ToolSetNotFound` | A tool set does not exist. |
| `Valid=False` | `ToolNameConflict` | Two different backend tools would be exposed under one name. Neither is. |
| `Ready=True` | `Available` | Every tool set is Ready. |
| `Ready=False` | `Invalid` | The capability is not valid. |
| `Ready=False` | `ToolSetNotReady` | Some tool sets are not Ready; the message says which and why. |

The same backend tool reached through two tool sets is one tool, not a conflict.

A conflict between exact names is a property of the manifests. One that patterns
cause exists only once a backend offers a tool both match — `*` on two servers
that each gain a `search` tool — so `Valid` can turn False without a spec
changing. The gateway exposes neither tool either way.

Printer columns: `ToolSets`, `Valid`, `Ready`, and `Reason` with `-o wide`.

---

## MCPGrant

Assigns capabilities to a subject. Short name `mcpg`.

```yaml
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPGrant
metadata:
  name: coding-agent
spec:
  subject: coding-agent
  capabilities:
    - code-research
    - artifact-authoring
```

### spec

| Field | Type | Required | Description |
|---|---|---|---|
| `subject` | string | yes | The subject the gateway derives from an access token. Opaque: see [Authentication](./authentication.md#the-subject). |
| `capabilities` | []string | yes | Names of `MCPCapability`s. At least one; no duplicates. |

Several grants may name the same subject. The subject gets the union.

### status

| Condition | Reason | Meaning |
|---|---|---|
| `Valid=True` | `Resolved` | Every capability exists, and no name is claimed twice. |
| `Valid=False` | `CapabilityNotFound` | A capability does not exist. It grants nothing; the others still apply. |
| `Valid=False` | `ToolNameConflict` | The grant's own capabilities claim one name for two backend tools. |
| `Ready=True` | `Available` | Every capability is Ready. |
| `Ready=False` | `Invalid` | The grant is not valid. |
| `Ready=False` | `CapabilityNotReady` | Some capabilities are not Ready. |
| `Ready=False` | `ToolNameConflict` | Together with the subject's other grants, a name is claimed twice. |

Printer columns: `Subject`, `Capabilities`, `Valid`, `Ready`, and `Reason` with
`-o wide`.
