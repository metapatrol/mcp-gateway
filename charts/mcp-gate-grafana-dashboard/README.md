# mcp-gate-grafana-dashboard

Grafana dashboard for mcp-gate (deployed as a ConfigMap for the Grafana sidecar)

## Chart Info

| Field | Value |
|---|---|
| Version | `1.3.0` |
| App Version | `1.3.0` |
| Type | `application` |

## Usage

This chart deploys the Grafana dashboard for mcp-gate as a ConfigMap. It is
designed to work with the [Grafana sidecar](https://github.com/grafana/helm-charts/tree/main/charts/grafana#sidecar-for-dashboards)
which auto-discovers dashboards via a configurable label.

The dashboard reads the metrics both mcp-gate components serve, so Prometheus has
to scrape them — `serviceMonitor.enabled` in the `mcp-gate` chart does that. It
finds the controller and the gateway by the `component` label of
`mcp_gate_build_info`, not by job name, so it works whatever the scrape jobs are
called.

| Row | Shows |
|---|---|
| Overview | Gateway replicas, call rate, failure ratio, unavailable backends, grants not ready, denied calls |
| Tool Calls | Rate, outcomes and latency by server; busiest, failing and slowest tools |
| Access | Denied calls by subject, refused tokens by reason |
| Backends | Availability over time, discovery failures, catalog sizes |
| Resources | The four kinds by Ready condition |
| Reconcilers | controller-runtime reconcile rate, errors, latency, work queues, API traffic |
| Runtime | Goroutines, memory and CPU per pod; build info |

### Example

```yaml
grafana:
  dashboard:
    enabled: true
    label: grafana_dashboard
    labelValue: "1"
    folderAnnotation: grafana_folder
    folder: "MCP Gate"
```

## Values

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| grafana.dashboard.enabled | bool | `true` | Enable the Grafana dashboard ConfigMap |
| grafana.dashboard.folder | string | `"MCP Gate"` | Grafana folder name. Leave empty to use the default folder (no annotation set) |
| grafana.dashboard.folderAnnotation | string | `"grafana_folder"` | Annotation key used to specify the Grafana folder |
| grafana.dashboard.label | string | `"grafana_dashboard"` | Label key used by the Grafana sidecar to discover dashboards |
| grafana.dashboard.labelValue | string | `"1"` | Label value used by the Grafana sidecar |
