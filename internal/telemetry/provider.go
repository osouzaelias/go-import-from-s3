package telemetry

import (
	"context"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc"
)

// InitProvider Initializes an OTLP exporter, and configures the corresponding trace and
// metric providers.
func InitProvider() func() {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			// the aws name used to display traces in backends
			semconv.ServiceNameKey.String("demo-server"),
		),
	)
	handleErr(err, "failed to create resource")

	otelAgentAddr, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if !ok {
		otelAgentAddr = "0.0.0.0:4317"
	}

	metricExp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(otelAgentAddr))
	handleErr(err, "Failed to create the collector metric exporter")

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(2*time.Second),
			),
		),
	)
	global.SetMeterProvider(meterProvider)

	traceClient := otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otelAgentAddr),
		otlptracegrpc.WithDialOption(grpc.WithBlock()))
	traceExp, err := otlptrace.New(ctx, traceClient)
	handleErr(err, "Failed to create the collector trace exporter")

	bsp := sdktrace.NewBatchSpanProcessor(traceExp)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetTracerProvider(tracerProvider)

	return func() {
		cxt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := traceExp.Shutdown(cxt); err != nil {
			otel.Handle(err)
		}
		// pushes any last exports to the receiver
		if err := meterProvider.Shutdown(cxt); err != nil {
			otel.Handle(err)
		}
	}
}

func handleErr(err error, message string) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}
