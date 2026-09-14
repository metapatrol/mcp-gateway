// Package metrics provides the Prometheus metrics of all three mcp-gate
// processes.
//
// They share one package and one prefix because they describe one system, and
// every metric is registered in controller-runtime's registry, which the
// controller and the gateway both serve. A process that never touches a metric
// simply reports no series for it — the gateway has no resource counts, the
// controller has no tool calls — and the job label tells the two apart.
package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// BuildInfo is a gauge that is always set to 1. Labels carry the static
	// build-time metadata.
	BuildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcp_gate_build_info",
			Help: "Build information for mcp-gate.",
		},
		[]string{"component", "version", "commit", "built_at", "go_version"},
	)

	// Resources counts the four kinds by their Ready condition. It is the
	// number an alert is written against: how many grants currently deliver
	// what they promise.
	Resources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcp_gate_resources",
			Help: "mcp-gate custom resources by kind and Ready condition status.",
		},
		[]string{"kind", "ready"},
	)

	// Discoveries counts catalog reads against a backend.
	Discoveries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcp_gate_discoveries_total",
			Help: "Catalog discoveries against MCP backends by server and outcome.",
		},
		[]string{"server", "outcome"},
	)

	// DiscoveredTools is the size of each backend's catalog as last read.
	DiscoveredTools = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcp_gate_discovered_tools",
			Help: "Tools in the most recently discovered catalog of each MCP backend.",
		},
		[]string{"server"},
	)

	// BackendAvailable is 1 while a backend's catalog is usable, 0 otherwise.
	BackendAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mcp_gate_backend_available",
			Help: "Whether the most recent discovery of an MCP backend succeeded (1) or not (0).",
		},
		[]string{"server"},
	)

	// ToolCalls counts forwarded tool calls.
	//
	// Only authorised calls land here, so the tool label is bounded by the
	// catalogs that grants select from. A refused call is counted in
	// DeniedToolCalls instead, without the name the caller sent — that name
	// is arbitrary input, and a label made of it would mint a series per typo.
	ToolCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcp_gate_tool_calls_total",
			Help: "Tool calls forwarded to MCP backends by server, tool and outcome.",
		},
		[]string{"server", "tool", "outcome"},
	)

	// ToolCallDuration measures forwarded tool calls end to end.
	ToolCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "mcp_gate_tool_call_duration_seconds",
			Help: "Duration of tool calls forwarded to MCP backends by server and tool.",
			// Tools run anything from a lookup to a build. A bucket set that
			// stops at a few seconds would report every long tool as simply
			// "slow".
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		},
		[]string{"server", "tool"},
	)

	// DeniedToolCalls counts tools/call requests refused by authorisation.
	DeniedToolCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcp_gate_tool_calls_denied_total",
			Help: "Tool calls refused because the subject holds no grant for the tool.",
		},
		[]string{"subject"},
	)

	// AuthFailures counts requests whose bearer token was refused.
	AuthFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mcp_gate_auth_failures_total",
			Help: "Requests refused at authentication by reason.",
		},
		[]string{"reason"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		BuildInfo, Resources, Discoveries, DiscoveredTools, BackendAvailable,
		ToolCalls, ToolCallDuration, DeniedToolCalls, AuthFailures,
	)
}

// SetBuildInfo sets the static build info gauge (call once at startup).
func SetBuildInfo(component, version, commit, builtAt string) {
	BuildInfo.WithLabelValues(component, version, commit, builtAt, runtime.Version()).Set(1)
}

// Condition statuses that are always published for every kind.
//
// A status nothing is in any more has to report zero rather than disappear: an
// absent series and "nothing is broken" look identical to an alert rule.
var readyStatuses = []string{"True", "False", "Unknown"}

// SetResources republishes the Ready tally for one kind.
func SetResources(kind string, byReady map[string]int) {
	for _, status := range readyStatuses {
		Resources.WithLabelValues(kind, status).Set(float64(byReady[status]))
	}
}

// Discovery outcomes.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// RecordDiscovery records one catalog read against a backend.
func RecordDiscovery(server string, tools int, err error) {
	if err != nil {
		Discoveries.WithLabelValues(server, OutcomeFailure).Inc()
		BackendAvailable.WithLabelValues(server).Set(0)
		return
	}
	Discoveries.WithLabelValues(server, OutcomeSuccess).Inc()
	BackendAvailable.WithLabelValues(server).Set(1)
	DiscoveredTools.WithLabelValues(server).Set(float64(tools))
}

// ForgetServer drops every per-server series of a backend that was
// unregistered, so a deleted MCPServer does not keep reporting its last state.
func ForgetServer(server string) {
	labels := prometheus.Labels{"server": server}
	Discoveries.DeletePartialMatch(labels)
	DiscoveredTools.DeletePartialMatch(labels)
	BackendAvailable.DeletePartialMatch(labels)
	ToolCalls.DeletePartialMatch(labels)
	ToolCallDuration.DeletePartialMatch(labels)
}

// Tool call outcomes.
const (
	// CallOK: the backend ran the tool and reported success.
	CallOK = "ok"
	// CallToolError: the backend ran the tool and the tool reported an error.
	CallToolError = "tool_error"
	// CallBackendError: the call did not complete — the backend was
	// unreachable, or answered with a protocol error.
	CallBackendError = "backend_error"
	// CallUnauthorized: the backend refused the call's credentials — the
	// caller's own token under Passthrough, the gateway's otherwise — or
	// they could not be obtained.
	CallUnauthorized = "unauthorized"
)

// RecordToolCall records one forwarded tool call.
func RecordToolCall(server, tool, outcome string, seconds float64) {
	ToolCalls.WithLabelValues(server, tool, outcome).Inc()
	ToolCallDuration.WithLabelValues(server, tool).Observe(seconds)
}

// RecordDenied records one refused tool call.
func RecordDenied(subject string) {
	DeniedToolCalls.WithLabelValues(subject).Inc()
}

// RecordAuthFailure records one refused bearer token.
func RecordAuthFailure(reason string) {
	AuthFailures.WithLabelValues(reason).Inc()
}
