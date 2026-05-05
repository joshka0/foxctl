# foxcular

`foxcular` is a standalone Go library for emitting structured foxcular events with trace/span correlation. It includes a core event/client API, span lifecycle helpers, sampling, default smart redaction, `slog` and `net/http` integrations, OTLP log export, persistence sinks, and tamper-evident audit chaining.

## Install

```sh
go get github.com/joshka0/foxcular
```

```go
import "github.com/joshka0/foxcular"
```

Optional integrations live in subpackages:

```go
import (
	"github.com/joshka0/foxcular/audit"
	"github.com/joshka0/foxcular/otlp"
	"github.com/joshka0/foxcular/persist"
)
```

## Core events and client

Create a `Client` with any `foxcular.Drain`. Events are immutable snapshots delivered through the drain after sampling and redaction.

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxcular"
)

func main() {
	drain := foxcular.DrainFunc(func(_ context.Context, e *foxcular.Event) error {
		fmt.Println(e.Operation, e.Status, e.Data["user_id"])
		return nil
	})

	client := foxcular.NewClient(drain, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer client.Close()

	_ = client.Emit("user.login").
		WithName("alice@example.com").
		WithData("user_id", 42).
		Success(context.Background(), 120*time.Millisecond)
}
```

Spans capture operation lifecycle and propagate trace/span context:

```go
ctx, span := client.StartSpan(context.Background(), "db.query",
	foxcular.WithSpanName("SELECT users"),
	foxcular.WithSpanData("db.system", "postgres"),
)
span.AddData("rows_affected", 42)
_ = span.End(nil)

_ = client.Emit("cache.get").
	InheritContext(ctx).
	WithData("key", "user:42").
	Success(ctx, 3*time.Millisecond)
```

## slog handler

`NewSlogHandler` converts `log/slog` records into foxcular events. When the context contains an active span, records are correlated to that span.

```go
handler := foxcular.NewSlogHandler(client, &foxcular.SlogHandlerOptions{
	Operation: "app.log",
})
logger := slog.New(handler)

logger.InfoContext(ctx, "processing request", "path", "/api/users")
```

## net/http middleware

`HTTPMiddleware` emits one request event per request and gives the downstream handler an active span context for correlated logging or event emission.

```go
mux := http.NewServeMux()
mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("hello"))
})

handler := foxcular.HTTPMiddleware(client, nil)(mux)
http.ListenAndServe(":8080", handler)
```

## OTLP log export

The `otlp` package maps foxcular events to OpenTelemetry log records through an SDK log exporter.

```go
drain := otlp.NewLogExporter(exporter, &otlp.LogExporterOptions{
	ResourceAttrs: map[string]string{
		"service.name": "my-service",
	},
})
defer drain.Close()

client := foxcular.NewClient(drain)
_ = client.Emit("order.created").
	WithName("order-123").
	WithData("amount", 99.99).
	Success(context.Background(), 0)
```

## Persistence

The `persist` package includes NDJSON and SQLite sinks.

```go
ndjsonSink, err := persist.NewNDJSONSink("events.ndjson")
if err != nil {
	panic(err)
}

sqliteSink, err := persist.NewSQLiteSink("events.db")
if err != nil {
	panic(err)
}
```

SQLite persistence uses `modernc.org/sqlite` and enables WAL mode, so SQLite may create sidecar files such as `events.db-wal` and `events.db-shm` next to the main database.

Events can be read back with:

```go
events, err := persist.ReadNDJSON("events.ndjson")
events, err = sqliteSink.QueryAllEvents()
```

## Audit hash chain

The `audit` package wraps any drain with an HMAC-SHA256 hash chain. Verification detects modified, removed, reordered, or wrongly keyed events.

```go
key := []byte("audit-secret-key-32-bytes-long!!")
auditDrain := audit.NewAuditDrain(key, ndjsonSink)

client := foxcular.NewClient(auditDrain)
_ = client.Emit("user.action").
	WithData("action", "login").
	Success(context.Background(), 0)

_ = client.Flush(context.Background())
_ = client.Close()

events, _ := persist.ReadNDJSON("events.ndjson")
if err := audit.Verify(key, events); err != nil {
	panic(err)
}
```

## Validation

```sh
mise exec go@1.26.1 -- go test ./...
mise exec go@1.26.1 -- go vet ./...
gofumpt -w .
golangci-lint run ./...
```
