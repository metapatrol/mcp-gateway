# CRD Reference

Group `mcp.gate.metapatrol.com`, version `v1alpha1`. All four kinds are
cluster-scoped and share the category `mcp-gate`:

```bash
kubectl get mcp-gate
kubectl explain mcptoolset.spec
```

Every kind reports `status.observedGeneration` and conditions of type `Valid`
and `Ready`. Their reasons are listed per kind below and are stable: alerts and
tooling may match on them.

---

## MCPServer

Registers an MCP backend. Short name `mcpsrv`.

```yaml
apiVersion: mcp.gate.metapatrol.com/v1alpha1
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
| `endpoint` | string | yes | URL of the backend's Streamable HTTP MCP endpoint. Must start with `http://` or `https://`. |
| `discoveryInterval` | duration | no | How often the catalog is re-read when the backend does not announce changes. Default `1m`; values below `10s` are raised to `10s`. |

There is no list of tools. The catalog is discovered.

### status

| Field | Description |
|---|---|
| `available` | Whether the most recent discovery succeeded. |
| `discoveredTools` | Number of tools in the last successful discovery. |
| `catalogRevision` | `sha256:…` over the tool definitions of the last successful discovery. |
| `lastDiscoveryTime` | When discovery last succeeded. |

While the backend is down, `discoveredTools`, `catalogRevision` and
`lastDiscoveryTime` keep describing the last catalog that was actually seen.

| Condition | Reason | Meaning |
|---|---|---|
| `Ready=True` | `Discovered` | `tools/list` answered. |
| `Ready=False` | `DiscoveryFailed` | Unreachable, or not an MCP server. The message has the error. |

Printer columns: `Endpoint`, `Available`, `Tools`, `Ready`.

---

## MCPToolSet

A curated subset of the tools of **one** server. Short name `mcpts`.

```yaml
apiVersion: mcp.gate.metapatrol.com/v1alpha1
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

### spec

| Field | Type | Required | Description |
|---|---|---|---|
| `serverRef` | string | yes | Name of the `MCPServer`. |
| `tools` | []string | yes | Tool names as the backend lists them. At least one; no duplicates. |

### status

| Field | Description |
|---|---|
| `catalogRevision` | The server catalog the tools were last verified against. |
| `missingTools` | Selected tools the server does not currently expose. |

| Condition | Reason | Meaning |
|---|---|---|
| `Valid=True` | `Resolved` | The server exists. |
| `Valid=False` | `ServerNotFound` | `serverRef` names no `MCPServer`. |
| `Ready=True` | `Available` | The server is Ready and exposes every selected tool. |
| `Ready=False` | `Invalid` | The tool set is not valid. |
| `Ready=False` | `ServerUnavailable` | The server is not Ready. |
| `Ready=False` | `MissingTools` | Some tools are not exposed — `"symbol_search" is not currently exposed by source-intelligence`. |
| `Ready=Unknown` | `CatalogPending` | The controller has not read the server's catalog since it started. |

The gateway exposes the tools that do exist; a missing tool does not take the
rest of the set with it.

Printer columns: `Server`, `Valid`, `Ready`, and `Reason` with `-o wide`.

---

## MCPCapability

A useful ability built from tool sets, possibly of several servers. Short name
`mcpcap`.

```yaml
apiVersion: mcp.gate.metapatrol.com/v1alpha1
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

Printer columns: `ToolSets`, `Valid`, `Ready`, and `Reason` with `-o wide`.

---

## MCPGrant

Assigns capabilities to a subject. Short name `mcpg`.

```yaml
apiVersion: mcp.gate.metapatrol.com/v1alpha1
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
