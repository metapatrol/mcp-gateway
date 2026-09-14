# mcp-gate

Kubernetes-native access layer for MCP backends — one governed MCP endpoint, tools granted per subject.

## Chart Info

| Field | Value |
|---|---|
| Version | `1.1.0` |
| App Version | `1.1.0` |
| Type | `application` |

## Values

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controller.affinity | object | `{}` |  |
| controller.env | list | `[]` |  |
| controller.healthProbeBindAddress | string | `":8081"` |  |
| controller.image.pullPolicy | string | `"IfNotPresent"` |  |
| controller.image.repository | string | `"zot.metapatrol.local/images/platform/mcp-gate-controller"` |  |
| controller.image.tag | string | `""` |  |
| controller.leaderElect | bool | `false` |  |
| controller.metricsBindAddress | string | `":8080"` |  |
| controller.nodeSelector | object | `{}` |  |
| controller.podAnnotations | object | `{}` |  |
| controller.replicaCount | int | `1` |  |
| controller.resources.limits.cpu | string | `"500m"` |  |
| controller.resources.limits.memory | string | `"256Mi"` |  |
| controller.resources.requests.cpu | string | `"50m"` |  |
| controller.resources.requests.memory | string | `"64Mi"` |  |
| controller.serviceAccount.annotations | object | `{}` |  |
| controller.serviceAccount.create | bool | `true` |  |
| controller.serviceAccount.name | string | `""` |  |
| controller.tolerations | list | `[]` |  |
| fullnameOverride | string | `""` |  |
| gateway.affinity | object | `{}` |  |
| gateway.authorizationServers | list | `[]` |  |
| gateway.callTimeout | string | `"5m"` |  |
| gateway.catalogMaxAge | string | `"5m"` |  |
| gateway.env | list | `[]` |  |
| gateway.healthProbeBindAddress | string | `":8081"` |  |
| gateway.image.pullPolicy | string | `"IfNotPresent"` |  |
| gateway.image.repository | string | `"zot.metapatrol.local/images/platform/mcp-gate-gateway"` |  |
| gateway.image.tag | string | `""` |  |
| gateway.mcpBindAddress | string | `":8090"` |  |
| gateway.mcpPath | string | `"/mcp"` |  |
| gateway.metricsBindAddress | string | `":8080"` |  |
| gateway.nodeSelector | object | `{}` |  |
| gateway.oauth.audience | string | `""` |  |
| gateway.oauth.introspection.cacheTTL | string | `"30s"` |  |
| gateway.oauth.introspection.clientID | string | `""` |  |
| gateway.oauth.introspection.clientSecretRef | object | `{}` |  |
| gateway.oauth.introspection.url | string | `""` |  |
| gateway.oauth.issuer | string | `""` |  |
| gateway.oauth.jwksURL | string | `""` |  |
| gateway.oauth.subjectClaim | string | `"sub"` |  |
| gateway.podAnnotations | object | `{}` |  |
| gateway.podDisruptionBudget.enabled | bool | `true` |  |
| gateway.podDisruptionBudget.maxUnavailable | int | `1` |  |
| gateway.replicaCount | int | `2` |  |
| gateway.resourceURL | string | `""` |  |
| gateway.resources.limits.cpu | string | `"1"` |  |
| gateway.resources.limits.memory | string | `"512Mi"` |  |
| gateway.resources.requests.cpu | string | `"100m"` |  |
| gateway.resources.requests.memory | string | `"128Mi"` |  |
| gateway.scopesSupported | list | `[]` |  |
| gateway.service.port | int | `80` |  |
| gateway.service.type | string | `"ClusterIP"` |  |
| gateway.serviceAccount.annotations | object | `{}` |  |
| gateway.serviceAccount.create | bool | `true` |  |
| gateway.serviceAccount.name | string | `""` |  |
| gateway.sessionTimeout | string | `"30m"` |  |
| gateway.stateless | bool | `false` |  |
| gateway.tolerations | list | `[]` |  |
| imagePullSecrets | list | `[]` |  |
| logging.format | string | `"json"` |  |
| nameOverride | string | `""` |  |
| networkPolicy.agentNamespaceSelector | object | `{}` |  |
| networkPolicy.agentPodSelector | object | `{}` |  |
| networkPolicy.enabled | bool | `false` |  |
| networkPolicy.monitoringNamespaceSelector | object | `{}` |  |
| rbac.create | bool | `true` |  |
| serviceMonitor.additionalLabels | object | `{}` |  |
| serviceMonitor.enabled | bool | `false` |  |
| serviceMonitor.honorLabels | bool | `false` |  |
| serviceMonitor.interval | string | `"30s"` |  |
| serviceMonitor.metricRelabelings | list | `[]` |  |
| serviceMonitor.namespace | string | `""` |  |
| serviceMonitor.relabelings | list | `[]` |  |
| serviceMonitor.scrapeTimeout | string | `""` |  |
