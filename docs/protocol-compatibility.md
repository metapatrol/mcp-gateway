# Protocol compatibility

Which MCP versions and transports work between an agent and the gateway, and
between the gateway and a backend — and what the gateway gives up for the
older ones.

## The matrix

| MCP version | Transport | Agent → gateway | Gateway → backend |
|---|---|---|---|
| **2026-07-28** | Streamable HTTP, no sessions | ✅ | ✅ preferred |
| 2025-11-25 | Streamable HTTP, sessions optional | ✅ without sessions | ✅ |
| 2025-06-18 | Streamable HTTP, sessions optional | ✅ without sessions | ✅ |
| 2025-03-26 | Streamable HTTP, sessions optional | ✅ without sessions | ✅ |
| 2024-11-05 | HTTP+SSE, sessions required | ❌ 400 | ✅ with `spec.transport: SSE` |
| any | stdio | ❌ | ❌ |

Towards agents, the gateway decides what it speaks. Towards backends, it takes
what the SDK can negotiate: a backend is someone else's software.

## Agents

### MCP 2026-07-28

The version the gateway is built for. It has no sessions: version and
capabilities travel with every request, and `server/discover` takes the place
of `initialize`. Every request carries:

- the header `MCP-Protocol-Version: 2026-07-28`,
- the header `Mcp-Method` with the JSON-RPC method,
- `io.modelcontextprotocol/protocolVersion`, `…/clientInfo` and
  `…/clientCapabilities` in `params._meta`.

An agent that wants to hear about changes keeps a `subscriptions/listen`
stream open.

### MCP 2025-03-26 to 2025-11-25

These versions make sessions optional, and the gateway issues none. An agent
starts with `initialize` as usual; the answer carries no `Mcp-Session-Id`, and
every later request stands on its own — on whichever replica it lands.

| Works | How |
|---|---|
| `initialize` | answered with the agent's version, the gateway's capabilities and the backends' instructions |
| `tools/list`, `tools/call` | as in 2026-07-28: filtered by the grants, authorised again on every call |
| Progress of a running call | on the response stream of that call |
| `notifications/tools/list_changed` | on the agent's `GET` stream, which the gateway ties to the subject by the token alone |

A 2025-03-26 client sends no `MCP-Protocol-Version` header; later versions
send theirs after `initialize`. A request without the header is taken for the
oldest of these versions.

The gateway sends no requests of its own to an agent — no sampling,
elicitation or roots — in any version, so nothing is lost there.

### What is refused

- **2024-11-05 (HTTP+SSE).** The client holds an event stream open and the
  answers arrive on it, by a session ID the server hands out. That needs a
  session on one replica, and with it sticky routing. A request naming
  2024-11-05 gets `400 Unsupported protocol version` with the supported list.
- **stdio.** The gateway is a network service. An agent that only speaks
  stdio needs a local bridge to Streamable HTTP;
  [`mcp-gateway-proxy`](authentication.md#mcp-gateway-proxy) is HTTP only.
- **Any other version** named in `MCP-Protocol-Version`: 400, with the
  supported list.

A request without a token gets the 401 first, whatever version it names, so
an agent still learns where to get one.

### Behind mcp-gateway-proxy

The proxy adds the token and passes the HTTP requests through unchanged, so
the matrix above holds for an agent behind it as well.

## Backends

The SDK negotiates the newest version both sides speak.

- **2026-07-28**: no sessions. A `Passthrough` backend shares the gateway's
  connection, each call with its caller's token.
- **2025-03-26 to 2025-11-25**: one session per connection, renewed when the
  backend restarts. A `Passthrough` backend gets one connection per caller
  token.
- **2024-11-05 (HTTP+SSE)**: set `spec.transport: SSE` on the `MCPServer`;
  every connection then holds an event stream open. See the
  [CRD reference](crd-reference.md).
- **stdio**: wrap the server in an HTTP server first.

How connections, listeners and retries work is in
[Architecture](architecture.md#protocol-and-replicas).

## When a new version comes out

The gateway accepts it next to the ones it speaks now, once the SDK does. A
version that brings sessions back would need them on one replica and sticky
routing — the reason 2024-11-05 is refused.
