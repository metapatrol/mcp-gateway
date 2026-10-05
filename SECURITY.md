# Security

## Reporting a vulnerability

Please report security problems privately through GitHub's
[private vulnerability reporting](https://github.com/metapatrol/mcp-gateway/security/advisories/new)
(*Security → Report a vulnerability*), not in a public issue or pull request.

Include what you found, how to reproduce it, and what an attacker could do with
it. You will get an answer as soon as a maintainer has looked at it.

## Scope

The gateway is the enforcement point between agents and MCP backends. Of
particular interest is anything that lets a caller:

- see or call a tool its grants do not reach, or keep calling one after the
  grant was revoked,
- get a token accepted that should be refused — wrong issuer, audience or
  signature, expired, inactive,
- tell a tool outside its grants from one that does not exist,
- make the gateway send its own credentials, or an agent's token, anywhere
  but the backend `spec.auth` names,
- make the gateway or the controller read a Secret outside their own
  namespace,
- read the inspect API without a ServiceAccount token it was configured to
  accept,
- reach `mcp-gateway-proxy` from anything but the local machine or pod.

The security model is described in [docs/authentication.md](docs/authentication.md),
[docs/architecture.md](docs/architecture.md#cluster-scope) and the
[design record](docs/design.md#21-principles).

## Supported versions

Fixes go into the next release; only the latest release is supported.
