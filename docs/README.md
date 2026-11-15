# Documentation

mcp-gateway puts one governed MCP endpoint in front of every MCP backend in a
Kubernetes cluster, and shows each agent exactly the tools its grants allow.

## Getting started

| | |
|---|---|
| [Installation](installation.md) | from an empty cluster to an agent calling its first tool |
| [FAQ](faq.md) | short answers to the questions that come up first |

## Using it

| | |
|---|---|
| [CRD Reference](crd-reference.md) | every field and condition of the four kinds |
| [Protocol compatibility](protocol-compatibility.md) | which MCP versions and transports work towards agents and backends, and what each gets |
| [Authentication](authentication.md) | token verification, the subject, protected resource metadata, credentials towards backends, mcp-gateway-proxy |
| [Console](console.md) | mcp-gateway-ui: enabling it, what it shows, how data and changes reach it |
| [Operations](operations.md) | health, metrics, alerts, audit records, what to do when something is not Ready, scaling, upgrades |
| [Grafana dashboard](../charts/mcp-gateway-grafana-dashboard/README.md) | the dashboard chart, and what each row shows |

## Understanding it

| | |
|---|---|
| [Architecture](architecture.md) | components, data flows, request flow, change detection, failure behaviour, the inspect API |
| [Design](design.md) | the design record: the model, the decisions and the principles behind them |

## Changing it

| | |
|---|---|
| [Development](development.md) | layout, building and testing, running it locally, releases |
| [Inspect API](../api/openapi/inspect.yaml) | the gateway's read-only API for the console, as OpenAPI |
| [Chart values](../charts/mcp-gateway/README.md) | the Helm chart's values, generated |
| [CI](../.forgejo/README.md) | what the workflows do and what they need |
