package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/go-logr/logr"
	"github.com/superfly/flyctl/internal/buildinfo"
	"github.com/superfly/flyctl/internal/logger"
	"github.com/superfly/flyctl/iostreams"
)

const (
	tracerName       = "github.com/superfly/flyctl"
	HeaderFlyTraceId = "fly-trace-id"
	HeaderFlySpanId  = "fly-span-id"
)

func getCollectorUrl() string {
	url := os.Getenv("FLY_TRACE_COLLECTOR_URL")
	if url != "" {
		return url
	}

	if buildinfo.IsDev() {
		return "fly-otel-collector-dev.fly.dev:4317"
	}

	return "fly-otel-collector-prod.fly.dev:4317"
}

func GetTracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

func RecordError(span trace.Span, err error, description string) {
	span.RecordError(err)
	span.SetStatus(codes.Error, description)
}

func CreateLinkSpan(ctx context.Context, res *http.Response) {
	remoteSpanCtx := SpanContextFromHeaders(res)
	_, span := GetTracer().Start(ctx, "flaps.link", trace.WithLinks(trace.Link{SpanContext: remoteSpanCtx}))
	defer span.End()
}

func SpanContextFromHeaders(res *http.Response) trace.SpanContext {
	traceIDstr := res.Header.Get(HeaderFlyTraceId)
	spanIDstr := res.Header.Get(HeaderFlySpanId)

	traceID, _ := trace.TraceIDFromHex(traceIDstr)
	spanID, _ := trace.SpanIDFromHex(spanIDstr)

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
}

func CMDSpan(ctx context.Context, appName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	startOpts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("app.name", appName),
		),
	}

	startOpts = append(startOpts, opts...)

	return GetTracer().Start(ctx, spanName, startOpts...)
}

func attachToken(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", os.Getenv("FLY_OTEL_AUTH_KEY"))
	return invoker(ctx, method, req, reply, cc, opts...)
}

func InitTraceProvider(ctx context.Context) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	switch {
	case os.Getenv("LOG_LEVEL") == "trace":
		stdoutExp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporter = stdoutExp
	default:
		grpcExpOpt := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(getCollectorUrl()),
			otlptracegrpc.WithDialOption(
				grpc.WithUnaryInterceptor(attachToken),
			),
		}
		grpcExpOpt = append(grpcExpOpt, otlptracegrpc.WithInsecure())

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		grpcExporter, err := otlptracegrpc.New(ctx, grpcExpOpt...)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to telemetry collector")
		}

		exporter = grpcExporter
	}

	resourceAttrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String("flyctl"),
		attribute.String("build.info.version", buildinfo.Version().String()),
		attribute.String("build.info.os", buildinfo.OS()),
		attribute.String("build.info.arch", buildinfo.Arch()),
		attribute.String("build.info.commit", buildinfo.Commit()),
	}

	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		resourceAttrs...,
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
	)

	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)
	otel.SetLogger(otelLogger(ctx))

	otel.SetErrorHandler(errorHandler(ctx))

	return tp, nil
}

func otelLogger(ctx context.Context) logr.Logger {
	io := iostreams.FromContext(ctx)

	var level slog.Level
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(io.ErrOut, opts)

	return logr.FromSlogHandler(handler)
}

func errorHandler(ctx context.Context) otel.ErrorHandler {
	logger := logger.FromContext(ctx)

	return otel.ErrorHandlerFunc(func(err error) {
		logger.Debug("trace exporter", "error", err)
	})
}
