package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func installOtlpPipeline(ctx context.Context) (func(), error) {
	exporter, err := otlp.NewExporter(ctx, otlpgrpc.NewDriver(
		otlpgrpc.WithInsecure(),
		otlpgrpc.WithEndpoint("otel-collector.otel.svc.cluster.local:55680"),
	))
	if err != nil {
		return nil, fmt.Errorf("otlp setup: create exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String("service-b"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp setup: create resource: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithConfig(
		sdktrace.Config{
			DefaultSampler: sdktrace.AlwaysSample(),
		},
	), sdktrace.WithResource(
		res,
	), sdktrace.WithSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(exporter),
	))
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() {
		ctx := context.TODO()
		err := traceProvider.Shutdown(ctx)
		if err != nil {
			otel.Handle(err)
		}
		err = exporter.Shutdown(ctx)
		if err != nil {
			otel.Handle(err)
		}
	}, nil
}

func main() {
	ctx := context.Background()

	shutdown, err := installOtlpPipeline(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown()

	tracer := otel.Tracer("svcb")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "brrr")
		defer span.End()

		span.AddEvent("eevee event")

		res, err := otelhttp.Get(ctx, "http://svcc.default.svc")
		if err != nil {
			log.Println(err)
			return
		} else if res.StatusCode != 200 {
			log.Println(err)
			return
		}
		defer res.Body.Close()
		io.Copy(w, res.Body)
	})

	http.ListenAndServe(":8080", otelhttp.NewHandler(mux, "inject"))
}
