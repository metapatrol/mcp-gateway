// Package tracing exports the gateway's spans over OTLP.
//
// It is configured by the standard OTEL_* environment variables alone and
// stays off unless an OTLP endpoint is set: without one, the global tracer
// provider is the no-op one and a span costs nothing.
package tracing

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Name is the instrumentation scope of the gateway's spans.
const Name = "forgejo.metapatrol.local/platform/mcp-gateway"

// Enabled reports whether the environment asks for traces to be exported.
func Enabled() bool {
	if os.Getenv("OTEL_SDK_DISABLED") == "true" || os.Getenv("OTEL_TRACES_EXPORTER") == "none" {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Setup installs the W3C trace context propagator, and, when Enabled, a tracer
// provider that exports to the OTLP endpoint over HTTP. The returned function
// flushes and stops it.
func Setup(ctx context.Context, service, version string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES come last, so they win.
	res, err := resource.Merge(
		resource.NewSchemaless(semconv.ServiceName(service), semconv.ServiceVersion(version)),
		resource.Environment(),
	)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
