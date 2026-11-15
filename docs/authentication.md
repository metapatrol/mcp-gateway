# Authentication

The gateway authenticates a caller and derives a **subject**. What the subject may
do is decided by `MCPGrant`s naming it, and by nothing in the token — scopes are
recorded, never used to authorise.

```text
client_id + client_secret ──> OAuth token endpoint ──> access token
                                                          │
                                   gateway: verify ───────┘
                                          │
                                          └─> subject ──> MCPGrant.spec.subject
```

Nothing in the gateway is specific to an OAuth provider. It reads registered
claims (RFC 7519, RFC 7662) and one configurable claim for the subject.

---

## Verifying tokens

Two methods, usable together. With both configured, a token shaped like a JWT is
verified as one, and anything else is introspected.

### JWT against a JWKS

```bash
mcp-gateway \
  --oauth-jwks-url=https://hydra.example.com/.well-known/jwks.json \
  --oauth-issuer=https://hydra.example.com/ \
  --oauth-audience=https://mcp-gateway.example.com/mcp
```

- The signature is checked against the provider's published keys. The
  algorithm comes from the key, never from the token, so `alg: none` and
  RSA/HMAC confusion are not possible.
- `exp` is required, `nbf` honoured, with 30s of clock skew.
- `iss` and `aud` are checked when configured. Configure both: without an
  audience, a token the same provider issued for any other service is accepted
  here.
- Keys are fetched on the first token, not at startup, so the gateway starts while
  the provider is still coming up. They are refreshed every 15 minutes, and when a
  token fails to verify — which is what a rotated key looks like — at most every
  30 seconds, so forged tokens cannot turn the gateway into a load generator
  against the provider.
- A token that verified is remembered, by a hash of the token, until its `exp`:
  an agent sends the same token on every request, and the signature check is the
  largest part of what the gateway does per call. The memory holds only for the
  key set the token was verified against — when a refresh brings a different set,
  a withdrawn key above all, every remembered token is verified again. Refused
  tokens are never remembered, and at most 10,000 tokens are.

With Ory Hydra this needs `strategies.access_token: jwt`.

### Opaque tokens by introspection (RFC 7662)

```bash
mcp-gateway \
  --oauth-introspection-url=http://hydra-admin.hydra.svc:4445/admin/oauth2/introspect \
  --oauth-issuer=https://hydra.example.com/
```

- `active: false` is refused; so is an active token whose `token_use` is not
  `access_token` (a refresh token is active too).
- `exp`, `nbf`, `iss` and `aud` are checked like a JWT's when present.
- Answers are cached by a hash of the token for `--oauth-introspection-cache-ttl`
  (30s), or until the token expires if that is sooner. **The TTL is also the
  longest a revoked token keeps working.**
- If the endpoint wants client authentication, set
  `--oauth-introspection-client-id` and provide the secret in
  `MCP_GATEWAY_INTROSPECTION_CLIENT_SECRET` or `--oauth-introspection-client-secret-file`
  (chart: `gateway.oauth.introspection.clientSecretRef`). Hydra's admin port
  wants none, and is protected by the network instead.

This is Hydra's default token strategy.

### Failures

| Situation | Response | `mcp_gateway_auth_failures_total{reason}` |
|---|---|---|
| No `Authorization: Bearer` | 401 | `missing_token` |
| Bad signature, wrong issuer or audience, expired, no subject … | 401 | `signature`, `issuer`, `audience`, `expired`, `no_subject`, … |
| JWKS or introspection endpoint unreachable | 500 | `unavailable` |
| Valid token, but `MCP-Protocol-Version` names a version the gateway does not speak | 400, listing the supported versions | — |

The provider being down is deliberately not a 401: a 401 tells a well-behaved
client its token is bad, and it would go and fetch a new one from the provider
that is down.

## The subject

```bash
--oauth-subject-claim=sub     # the default
```

The subject is the value of that claim. A claim name that exists as a top-level
claim is read whole, even if it contains dots (URL-named claims are common);
otherwise a dotted name is followed into nested objects — `ext.subject` reads
`{"ext": {"subject": "coding-agent"}}`.

For the client credentials grant, `sub` is normally the OAuth client ID. The grant
then names the client ID:

```yaml
kind: MCPGrant
spec:
  subject: coding-agent        # == client_id
```

### When the provider generates client IDs

Ory Hydra v2 generates client IDs itself; a client created through hydra-maester
gets an ID like `4f1c9a1e-…`, known only after it exists
([hydra-maester#117](https://github.com/ory/hydra-maester/issues/117)). Two ways
to keep grants readable:

1. **Name the ID in the grant.** Read `CLIENT_ID` from the Secret hydra-maester
   writes and put it into `spec.subject`. Simple; the manifest depends on a
   generated value.
2. **Put a stable subject into the token.** Configure the provider to add a claim
   — with Hydra, a token hook that sets `ext.subject` from the client's metadata —
   and run the gateway with `--oauth-subject-claim=ext.subject`. Grants then name
   `coding-agent`, whatever the client ID is.

Either way the grant stays provider-neutral: `spec.subject` is an opaque string,
and which claim it came from is the gateway's configuration, not the resource's.

## Protected resource metadata (RFC 9728)

```bash
--resource-url=https://mcp-gateway.example.com/mcp \
--authorization-servers=https://hydra.example.com/
```

With both set, the gateway serves its metadata at
`/.well-known/oauth-protected-resource/mcp` (and at the root form), and every 401
carries

```text
WWW-Authenticate: Bearer resource_metadata="https://mcp-gateway.example.com/.well-known/oauth-protected-resource/mcp"
```

so an OAuth-capable MCP client can find the authorization server on its own.

## Every request on its own

The gateway keeps no sessions, in any version it speaks, so there is nothing to
bind a subject to beyond the request. Every request is authenticated with the token it carries, and every
`tools/call` is authorised against the subject of that token, at the moment it
arrives. A `subscriptions/listen` stream — or the `GET` stream of an agent of a
session-based version — is authenticated when it is opened, and tells its
holder only about the tools of the subject it was opened for.

## Towards backends

Everything above is how an agent gets into the gateway. How the gateway gets into
a backend is a separate matter, configured per backend in `MCPServer.spec.auth`
(fields: [CRD Reference](./crd-reference.md#specauth)).

| `type` | The backend sees | Suits |
|---|---|---|
| `None` | no credentials | a backend protected by the network alone |
| `ClientCredentials` | the gateway's own OAuth token | a backend that trusts the gateway to have authorised the call |
| `APIKey` | a static key from a Secret | the same, for a backend with keys instead of OAuth |
| `Passthrough` | the calling agent's own token | a backend that decides per caller itself |

With `ClientCredentials` and `APIKey`, the backend learns that the gateway called,
not which agent: the grant was the gateway's to check. With `Passthrough` it gets
the agent's token and makes its own decision; the gateway's grant still has to
allow the call first.

### Passthrough

The agent's `Authorization` header is sent to the backend unchanged, on that
agent's tool calls. The gateway checks nothing further about it — not the
audience, not the scopes. A token issued for the gateway reaches the backend with
the gateway as audience; whether the backend accepts that is its call, and a
backend that should only accept tokens meant for it has to say so itself.

- **Explicit, per backend.** A token is never forwarded to a backend whose type
  is not `Passthrough`.
- **Each call with its own token.** A backend speaking MCP 2026-07-28 gets every
  call on one shared connection, each request with its caller's token. A
  session-based backend binds a session to the identity that opened it, so there
  each distinct token gets a connection of its own for its calls; a replaced token
  starts a new one, and connections without calls for five minutes are closed.
- **Catalog reads have no agent.** The controller and the gateway read the
  catalog in the background, with `passthrough.discovery` — an API key or a
  client of their own — or with no credentials at all. A backend like engram,
  which lists its tools to anyone and guards the calls, needs no discovery
  credential.
- **A refused token is the agent's to know about.** The tool call returns
  `isError: true` with *the backend serving this tool refused the caller's
  credentials (HTTP 401 Unauthorized)* — or 403 — and nothing about the backend's
  address.

### Credentials the gateway holds

- **Secrets by name and key.** `{name, key}` points at one data key of one Secret,
  whatever the Secret's layout. They are read from the gateway's and the
  controller's own namespace only: the chart grants both `get` on Secrets there,
  and nothing elsewhere.
- **Read when needed.** A missing Secret does not stop the backend from being
  registered; its discovery fails with `CredentialsUnavailable`, naming the
  Secret and listing the keys it has, and recovers once the Secret exists.
- **Refreshed on refusal.** A 401 drops the token, or re-reads the Secret of an
  API key, and sends the request once more. A backend that still refuses makes
  the server `Unauthorized`; tool calls to it return the usual "currently
  unavailable" tool error, and the cause is in the gateway's log.
- **Only to the backend.** Credentials go to the scheme and host of
  `spec.endpoint`; a redirect elsewhere does not take them along.

A refusal never costs the session other callers share: the SDK would end a
session on an HTTP error, so the gateway turns 401 and 403 into an error of its
own before the SDK sees them. Refused calls count as
`mcp_gateway_tool_calls_total{outcome="unauthorized"}`.

### With Ory Hydra and hydra-maester

```yaml
apiVersion: hydra.ory.sh/v1alpha1
kind: OAuth2Client
metadata:
  name: mcp-gateway-memory
  namespace: mcp-system              # where mcp-gateway runs
spec:
  grantTypes: [client_credentials]
  scope: memory.read
  secretName: mcp-gateway-memory     # hydra-maester writes CLIENT_ID and CLIENT_SECRET
---
apiVersion: mcp-gateway.metapatrol.com/v1alpha1
kind: MCPServer
metadata:
  name: memory
spec:
  endpoint: http://memory.memory.svc/mcp
  auth:
    type: ClientCredentials
    clientCredentials:
      tokenURL: https://hydra.example.com/oauth2/token
      clientIDRef:     {name: mcp-gateway-memory, key: CLIENT_ID}
      clientSecretRef: {name: mcp-gateway-memory, key: CLIENT_SECRET}
      scopes: [memory.read]
```

## mcp-gateway-proxy

For agents without OAuth. The agent talks to the proxy on loopback, without
credentials; the proxy runs the client credentials grant and forwards everything
to the gateway with a bearer token.

```bash
mcp-gateway-proxy \
  --gateway-url=https://mcp-gateway.example.com/mcp \
  --token-url=https://hydra.example.com/oauth2/token \
  --client-id=coding-agent \
  --client-secret-file=/var/run/secrets/oauth/CLIENT_SECRET
```

| Flag | Environment | Default |
|---|---|---|
| `--listen-address` | `MCP_GATEWAY_PROXY_LISTEN_ADDRESS` | `127.0.0.1:8765` |
| `--gateway-url` | `MCP_GATEWAY_URL` | — |
| `--token-url` | `MCP_GATEWAY_TOKEN_URL` | — |
| `--client-id` | `MCP_GATEWAY_CLIENT_ID` | — |
| `--client-secret-file` | `MCP_GATEWAY_CLIENT_SECRET_FILE`, or the secret itself in `MCP_GATEWAY_CLIENT_SECRET` | — |
| `--scopes` | `MCP_GATEWAY_SCOPES` | none |
| `--audience` | `MCP_GATEWAY_AUDIENCE` | none |
| `--allowed-hosts` | `MCP_GATEWAY_PROXY_ALLOWED_HOSTS` | loopback names only |

What it does:

- **Forwards HTTP, not MCP.** Streamed responses and `subscriptions/listen` streams
  pass through untouched.
- **Caches the token** and replaces it 30 seconds before it expires.
- **Retries a refused token once.** On a 401 from the gateway — a token revoked
  before its `exp` — it fetches a new token and replays the request.
- **Keeps OAuth away from the agent.** A 401 that survives the retry means the
  proxy's own credentials are refused; it answers 502 with a plain message instead
  of passing on a challenge that would start an OAuth flow in the agent.
- **Drops what the agent sends** in `Authorization` and `Cookie`.

What it is not: a security boundary. Anything that can reach its port acts with
its client's grants. It therefore listens on loopback by default, refuses
requests whose `Host` is not a loopback name (DNS rebinding), and refuses
cross-origin browser requests.
