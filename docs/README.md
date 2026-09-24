# Documentation

- **[Concept](../CONCEPT.md)** — what mcp-gateway is for, and the principles it keeps
- **[Architecture](./architecture.md)** — components, data flows, request flow, change detection, failure behaviour
- **[Authentication](./authentication.md)** — token verification, the subject, protected resource metadata, credentials towards backends (`spec.auth`), mcp-gateway-proxy
- **[CRD Reference](./crd-reference.md)** — every field and condition of the four kinds
- **[Console](./console.md)** — mcp-gateway-ui: enabling it, what it shows, how data and changes reach it, `make dev-stack`
- **[Inspect API](../api/openapi/inspect.yaml)** — the gateway's read-only API for the console, as OpenAPI; see [Architecture](./architecture.md#inspect-api)
- **[Chart values](../charts/mcp-gateway/README.md)** — the Helm chart
- **[Grafana dashboard](../charts/mcp-gateway-grafana-dashboard/README.md)** — the dashboard chart, and what each row shows
- **[CI](../.forgejo/README.md)** — what the workflows do and what they need
