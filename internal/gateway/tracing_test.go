package gateway

import (
	"net/http"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// recordSpans makes the global tracer provider one that keeps every span, as
// tracing.Setup would with an exporter, for the length of the test.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return recorder
}

// traceparents records the traceparent headers a backend received with
// tools/call requests.
type traceparents struct {
	mu   sync.Mutex
	seen []string
}

func (p *traceparents) guard(r *http.Request) int {
	if mcptest.RPCMethod(r) == "tools/call" {
		p.mu.Lock()
		p.seen = append(p.seen, r.Header.Get("traceparent"))
		p.mu.Unlock()
	}
	return 0
}

// withTraceparent sends every request with the given traceparent, as an agent
// harness inside a trace of its own does.
type withTraceparent struct {
	base        http.RoundTripper
	traceparent string
}

func (w withTraceparent) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("traceparent", w.traceparent)
	return w.base.RoundTrip(r)
}

func attributes(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	out := map[attribute.Key]string{}
	for _, kv := range span.Attributes() {
		out[kv.Key] = kv.Value.String()
	}
	return out
}

func endedNamed(recorder *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

const (
	callerTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	callerSpan  = "00f067aa0ba902b7"
)

// A call continues the caller's trace, and the backend receives it in turn: a
// harness's trace covers model call, tool call and backend.
func TestTracing_ACallContinuesTheCallersTrace(t *testing.T) {
	recorder := recordSpans(t)
	e := newEnv(t, Options{})
	backend := &traceparents{}
	e.artifact.Guard(backend.guard)

	session := e.connectThrough(withTraceparent{base: bearer("token-coding-agent"), traceparent: "00-" + callerTrace + "-" + callerSpan + "-01"})

	res, err := call(session, "artifact_get")
	require.NoError(t, err)
	require.False(t, res.IsError, text(res))

	spans := endedNamed(recorder, "tools/call artifact_get")
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, callerTrace, span.SpanContext().TraceID().String())
	assert.Equal(t, callerSpan, span.Parent().SpanID().String())
	assert.True(t, span.Parent().IsRemote())
	assert.Equal(t, trace.SpanKindServer, span.SpanKind())
	attrs := attributes(span)
	assert.Equal(t, "coding-agent", attrs["user.id"])
	assert.Equal(t, "artifact", attrs["mcp.server"])
	assert.Equal(t, "artifact_get", attrs["mcp.tool"])
	assert.Equal(t, metrics.CallOK, attrs["mcp.outcome"])
	assert.Equal(t, codes.Unset, span.Status().Code)

	backend.mu.Lock()
	defer backend.mu.Unlock()
	require.Len(t, backend.seen, 1)
	assert.Equal(t, "00-"+callerTrace+"-"+span.SpanContext().SpanID().String()+"-01", backend.seen[0],
		"the backend's parent is the gateway's span")
}

// A call outside any trace starts one, and a refused call leaves a span too,
// with no more in it than the refusal.
func TestTracing_ARefusedCallIsTraced(t *testing.T) {
	recorder := recordSpans(t)
	e := newEnv(t, Options{})

	_, err := call(e.connect("token-nobody"), "artifact_get")
	require.Error(t, err)

	spans := endedNamed(recorder, "tools/call artifact_get")
	require.Len(t, spans, 1)
	assert.False(t, spans[0].Parent().IsValid(), "a trace of its own")
	attrs := attributes(spans[0])
	assert.Equal(t, outcomeDenied, attrs["mcp.outcome"])
	assert.NotContains(t, attrs, attribute.Key("mcp.server"), "nothing about a tool the caller holds no grant on")
}

func TestTracing_ABackendFailureIsAnError(t *testing.T) {
	recorder := recordSpans(t)
	e := newEnv(t, Options{})
	e.artifact.Guard(func(r *http.Request) int {
		if mcptest.RPCMethod(r) == "tools/call" {
			return http.StatusForbidden
		}
		return 0
	})

	res, err := call(e.connect("token-coding-agent"), "artifact_get")
	require.NoError(t, err)
	require.True(t, res.IsError)

	spans := endedNamed(recorder, "tools/call artifact_get")
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, metrics.CallUnauthorized, attributes(spans[0])["mcp.outcome"])
}

func TestToolCallsAreCountedBySubject(t *testing.T) {
	e := newEnv(t, Options{})
	counted := func() float64 {
		return testutil.ToFloat64(metrics.SubjectToolCalls.WithLabelValues("coding-agent", metrics.CallOK))
	}
	before := counted()

	res, err := call(e.connect("token-coding-agent"), "artifact_get")
	require.NoError(t, err)
	require.False(t, res.IsError, text(res))
	assert.Equal(t, before+1, counted())
}
