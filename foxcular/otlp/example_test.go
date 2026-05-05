package otlp_test

import (
	"context"
	"fmt"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/otlp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
)

// exampleExporter captures exported log records for inspection in examples.
type exampleExporter struct {
	records []sdklog.Record
}

func (e *exampleExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.records = append(e.records, records...)
	return nil
}

func (e *exampleExporter) ForceFlush(_ context.Context) error { return nil }
func (e *exampleExporter) Shutdown(_ context.Context) error   { return nil }

// Example_logExporter demonstrates mapping foxcular events to OTLP log records.
func Example_logExporter() {
	exporter := &exampleExporter{}
	drain := otlp.NewLogExporter(exporter, &otlp.LogExporterOptions{
		ResourceAttrs: map[string]string{
			"service.name": "my-service",
		},
	})
	defer func() { _ = drain.Close() }()

	client := foxcular.NewClient(drain, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	err := client.Emit("order.created").
		WithName("order-123").
		WithData("amount", 99.99).
		WithData("currency", "USD").
		Success(context.Background(), 0)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	_ = client.Flush(context.Background())

	for _, r := range exporter.records {
		factory := logtest.RecordFactory{Body: r.Body()}
		fmt.Printf("body=%v\n", factory.Body)
	}
	// Output:
	// body=order.created
}

// Example_logExporter_error demonstrates error event mapping to OTLP severity.
func Example_logExporter_error() {
	exporter := &exampleExporter{}
	drain := otlp.NewLogExporter(exporter, nil)
	defer func() { _ = drain.Close() }()

	client := foxcular.NewClient(drain, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	_ = client.Emit("payment.charge").
		Error(context.Background(), fmt.Errorf("card declined"), 0)

	_ = client.Flush(context.Background())

	fmt.Printf("records=%d\n", len(exporter.records))
	// Output:
	// records=1
}
