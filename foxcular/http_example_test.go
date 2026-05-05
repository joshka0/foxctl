package foxcular_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/joshka0/foxcular"
)

// Example_httpMiddleware demonstrates net/http middleware emitting request events.
func Example_httpMiddleware() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})

	handler := foxcular.HTTPMiddleware(client, nil)(mux)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s status=%s method=%v path=%v http_status=%v\n",
			e.Operation, e.Status,
			e.Data["http.method"], e.Data["http.path"], e.Data["http.status"])
	}
	// Output:
	// op=http.request status=ok method=GET path=/hello http_status=200
}

// Example_httpMiddleware_error demonstrates middleware capturing server errors.
func Example_httpMiddleware_error() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})

	handler := foxcular.HTTPMiddleware(client, nil)(mux)

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s status=%s http_status=%v\n",
			e.Operation, e.Status, e.Data["http.status"])
	}
	// Output:
	// op=http.request status=error http_status=500
}

// Example_httpMiddleware_contextPropagation demonstrates that the handler
// receives a context with an active span for correlated logging.
func Example_httpMiddleware_contextPropagation() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/with-log", func(w http.ResponseWriter, r *http.Request) {
		span := foxcular.ActiveSpanFromContext(r.Context())
		if span != nil {
			fmt.Printf("span_active=true trace_set=%v\n", span.TraceID() != "")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := foxcular.HTTPMiddleware(client, nil)(mux)

	req := httptest.NewRequest(http.MethodGet, "/with-log", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	_ = client.Flush(context.Background())

	// Output:
	// span_active=true trace_set=true
}
