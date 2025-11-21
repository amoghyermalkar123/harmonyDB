package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

// InitTracing initializes OpenTelemetry tracing
func InitTracing(serviceName string) (func(), error) {
	// Create stdout exporter for development
	// In production, replace with OTLP exporter
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	// Create resource with service information
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("harmonydb")

	// Return shutdown function
	return func() {
		tp.Shutdown(context.Background())
	}, nil
}

// GetTracer returns the global tracer
func GetTracer() trace.Tracer {
	if tracer == nil {
		tracer = otel.Tracer("harmonydb")
	}
	return tracer
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, name, opts...)
}

// Common span attributes
func KeyAttr(key string) attribute.KeyValue {
	return attribute.String("db.key", key)
}

func NodeIDAttr(nodeID int64) attribute.KeyValue {
	return attribute.Int64("raft.node_id", nodeID)
}

func TermAttr(term int64) attribute.KeyValue {
	return attribute.Int64("raft.term", term)
}

func LogIDAttr(logID int64) attribute.KeyValue {
	return attribute.Int64("raft.log_id", logID)
}

func PeerIDAttr(peerID int64) attribute.KeyValue {
	return attribute.Int64("raft.peer_id", peerID)
}

func OperationAttr(op string) attribute.KeyValue {
	return attribute.String("db.operation", op)
}

func EntriesCountAttr(count int) attribute.KeyValue {
	return attribute.Int("raft.entries_count", count)
}
