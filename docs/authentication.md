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
mcp-gate-gateway \
  --oauth-jwks-url=https://hydra.example.com/.well-known/jwks.json \
  --oauth-issuer=https://hydra.example.com/ \
  --oauth-audience=https://mcp-gate.example.com/mcp
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

With Ory Hydra this needs `strategies.access_token: jwt`.

### Opaque tokens by introspection (RFC 7662)

```bash
mcp-gate-gateway \
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
  `MCP_GATE_INTROSPECTION_CLIENT_SECRET` or `--oauth-introspection-client-secret-file`
  (chart: `gateway.oauth.introspection.clientSecretRef`). Hydra's admin port
  wants none, and is protected by the network instead.

This is Hydra's default token strategy.

### Failures

| Situation | Response | `mcp_gate_auth_failures_total{reason}` |
|---|---|---|
| No `Authorization: Bearer` | 401 | `missing_token` |
| Bad signature, wrong issuer or audience, expired, no subject … | 401 | `signature`, `issuer`, `audience`, `expired`, `no_subject`, … |
| JWKS or introspection endpoint unreachable | 500 | `unavailable` |

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
--resource-url=https://mcp-gate.example.com/mcp \
--authorization-servers=https://hydra.example.com/
```

With both set, the gateway serves its metadata at
`/.well-known/oauth-protected-resource/mcp` (and at the root form), and every 401
carries

```text
WWW-Authenticate: Bearer resource_metadata="https://mcp-gate.example.com/.well-known/oauth-protected-resource/mcp"
```

so an OAuth-capable MCP client can find the authorization server on its own.

## Session binding

A session belongs to the subject whose token opened it. A request carrying the
session ID with a different subject's token is refused with 403. On top of that,
every `tools/call` is authorised against the subject of the token on that very
request.

## mcp-gate-proxy

For agents without OAuth. The agent talks to the proxy on loopback, without
credentials; the proxy runs the client credentials grant and forwards everything
to the gateway with a bearer token.

```bash
mcp-gate-proxy \
  --gateway-url=https://mcp-gate.example.com/mcp \
  --token-url=https://hydra.example.com/oauth2/token \
  --client-id=coding-agent \
  --client-secret-file=/var/run/secrets/oauth/CLIENT_SECRET
```

| Flag | Environment | Default |
|---|---|---|
| `--listen-address` | `MCP_GATE_PROXY_LISTEN_ADDRESS` | `127.0.0.1:8765` |
| `--gateway-url` | `MCP_GATE_GATEWAY_URL` | — |
| `--token-url` | `MCP_GATE_TOKEN_URL` | — |
| `--client-id` | `MCP_GATE_CLIENT_ID` | — |
| `--client-secret-file` | `MCP_GATE_CLIENT_SECRET_FILE`, or the secret itself in `MCP_GATE_CLIENT_SECRET` | — |
| `--scopes` | `MCP_GATE_SCOPES` | none |
| `--audience` | `MCP_GATE_AUDIENCE` | none |
| `--allowed-hosts` | `MCP_GATE_PROXY_ALLOWED_HOSTS` | loopback names only |

What it does:

- **Forwards HTTP, not MCP.** Sessions, streamed responses and notifications pass
  through untouched; it does not care which protocol version the two ends speak.
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
