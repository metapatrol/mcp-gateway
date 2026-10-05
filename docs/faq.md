# FAQ

**Contents**

- [Isn't this just an MCP reverse proxy?](#isnt-this-just-an-mcp-reverse-proxy)
- [Why not list the tools in the resources?](#why-not-list-the-tools-in-the-resources)
- [Why four kinds and not one?](#why-four-kinds-and-not-one)
- [Why are they cluster-scoped?](#why-are-they-cluster-scoped)
- [Does it need Ory Hydra?](#does-it-need-ory-hydra)
- [Which MCP versions does it speak?](#which-mcp-versions-does-it-speak)
- [Our agent cannot do OAuth. Can it still connect?](#our-agent-cannot-do-oauth-can-it-still-connect)
- [Is the proxy a security boundary?](#is-the-proxy-a-security-boundary)
- [What does an agent see of tools it was not granted?](#what-does-an-agent-see-of-tools-it-was-not-granted)
- [Is a revoked grant effective at once?](#is-a-revoked-grant-effective-at-once)
- [Does the gateway forward the agent's token to backends?](#does-the-gateway-forward-the-agents-token-to-backends)
- [Two backends have a tool called `search`. What happens?](#two-backends-have-a-tool-called-search-what-happens)
- [Are wildcard patterns safe?](#are-wildcard-patterns-safe)
- [What happens when a backend is down?](#what-happens-when-a-backend-is-down)
- [What happens when the controller is down?](#what-happens-when-the-controller-is-down)
- [Can we run several gateway replicas?](#can-we-run-several-gateway-replicas)
- [Does it aggregate prompts and resources too?](#does-it-aggregate-prompts-and-resources-too)
- [Who can open the console?](#who-can-open-the-console)

## Isn't this just an MCP reverse proxy?

A reverse proxy passes everything through. The gateway decides, per caller,
which tools exist at all: it assembles one catalog from many backends, shows
each subject exactly the tools its grants reach, authorises every call again
when it arrives, and holds the backends' credentials so agents never see them.
See [design §1](design.md#1-purpose).

## Why not list the tools in the resources?

Because the backend already does. A copied list is a second source of truth
that goes stale the day a backend changes. The gateway and the controller
discover catalogs over MCP, and resources only select from them — by name, or
by pattern. See [design §8](design.md#8-discovery).

## Why four kinds and not one?

Each is one concern, often owned by someone else: where a backend is
(`MCPServer`), which of its tools are fit to hand out (`MCPToolSet`), what
makes a useful ability (`MCPCapability`), and who holds it (`MCPGrant`). A
tool set is reused by many capabilities, and a capability by many grants.

## Why are they cluster-scoped?

The gateway reaches every backend it routes to, and a subject is not issued
to a namespace. Namespaced resources would let any tenant register another
team's backend and grant themselves access to it. See
[architecture: cluster scope](architecture.md#cluster-scope).

## Does it need Ory Hydra?

No. It needs an OAuth 2.0 provider whose access tokens it can verify — JWTs
against a published key set, or opaque tokens by RFC 7662 introspection — and
reads nothing provider-specific. The documentation shows Hydra because the
maintainers use it. See [authentication](authentication.md).

## Which MCP versions does it speak?

Towards agents, **MCP 2026-07-28** only. It has no sessions, so any replica
answers any request; clients of the older, session-based versions are
refused. Towards backends, whatever they speak: the SDK negotiates
2026-07-28 or the backend's newest session-based version.

## Our agent cannot do OAuth. Can it still connect?

Yes, through `mcp-gateway-proxy`: a small process beside the agent — a sidecar,
or on a workstation — that offers an MCP endpoint without authentication on
loopback, obtains tokens with the client credentials grant and forwards
everything to the gateway. The gateway cannot tell the difference. See
[authentication: mcp-gateway-proxy](authentication.md#mcp-gateway-proxy).

## Is the proxy a security boundary?

No. Anything that reaches its port acts with its client's grants. That is why
it listens on loopback, refuses `Host` names that are not loopback, and
refuses cross-origin browser requests. The boundary is the gateway.

## What does an agent see of tools it was not granted?

Nothing. They are not in its `tools/list`, the instructions it is sent come
only from backends it holds a tool from, and a call to such a tool gets the
same error as a call to a tool that does not exist — so it cannot probe for
them.

## Is a revoked grant effective at once?

Yes, as soon as the gateway's watch has delivered the change. Every `tools/call` is authorised against the current
policy, not against the tool list the agent received earlier. With opaque
tokens, a token revoked at the provider keeps working until the introspection
cache expires (30 seconds by default).

## Does the gateway forward the agent's token to backends?

Only to a backend whose `MCPServer` says `spec.auth.type: Passthrough`, and
only on that agent's own calls. Otherwise backends see no credentials, a token
of the gateway's own OAuth client, or an API key — never the agent's token.
See [authentication: towards backends](authentication.md#towards-backends).

## Two backends have a tool called `search`. What happens?

A subject granted both gets neither, and the capability or grant reports
`ToolNameConflict` naming both. Rename one of them in its tool set — a
`prefix` for every tool, or `aliases` for single ones. A subject granted only
one of them is not affected.

## Are wildcard patterns safe?

They select what the backend offers now, including tools it adds later —
convenient for `code.*`, risky for anything that writes. Give such a tool set
an `exclude` for what must never be granted by accident, and check
`status.resolvedTools`, or the console, for what it selects.

## What happens when a backend is down?

Its tools stay listed for `catalogMaxAge` (five minutes by default) and calls
to them return a tool error the model can see; then they drop out of tool
lists until discovery succeeds again. Other backends are not affected. See
[architecture: failure behaviour](architecture.md#failure-behaviour).

## What happens when the controller is down?

Nothing changes for agents. The gateway watches the resources and discovers
backends itself; the controller only writes status, which goes stale.

## Can we run several gateway replicas?

Yes, and the chart runs two. Any replica answers any request, and every
replica keeps each subject's tools in step with the API on its own. No
ingress affinity is needed.

## Does it aggregate prompts and resources too?

No. The gateway aggregates tools, and passes on the backends' instructions.
Prompts, resources, sampling and elicitation are not supported.

## Who can open the console?

Whoever can reach its Service: it has no sign-in of its own, and shows every
subject's grants and every backend's catalog. Put a port-forward or an
authenticating proxy in front of it, and treat access to it like read access
to the four kinds. See [console](console.md#opening-it).
