package foxcular

import (
	"errors"
	"net/http"
)

// HTTPMiddlewareOptions configures the behaviour of the HTTP middleware.
type HTTPMiddlewareOptions struct {
	// Operation is the event operation name. Defaults to "http.request".
	Operation string
}

// HTTPMiddleware returns an HTTP middleware that emits a foxcular event for each
// request via the given foxcular Client. The middleware captures method, path,
// response status, duration, errors, panics, and propagates span context to
// the downstream handler.
//
// When the request context carries an active trace ID, the middleware preserves
// it. Otherwise a new trace is started. The handler receives a context with an
// active span so that any logging or emission within the handler correlates
// with the request event.
func HTTPMiddleware(client *Client, opts *HTTPMiddlewareOptions) func(http.Handler) http.Handler {
	if opts == nil {
		opts = &HTTPMiddlewareOptions{}
	}
	if opts.Operation == "" {
		opts.Operation = "http.request"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := client.clock.Now()

			// Start a span for this request.
			ctx, span := client.StartSpan(
				r.Context(), opts.Operation,
				WithSpanName(r.Method+" "+r.URL.RequestURI()),
				WithSpanData("http.method", r.Method),
				WithSpanData("http.path", r.URL.RequestURI()),
				WithSpanData("http.host", r.Host),
			)

			// Wrap the response writer to capture the status code.
			rw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Recover from panics.
			var panicErr error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						switch v := rec.(type) {
						case error:
							panicErr = v
						case string:
							panicErr = errors.New(v)
						default:
							panicErr = errors.New("panic recovered")
						}
					}
				}()
				next.ServeHTTP(rw, r.WithContext(ctx))
			}()

			// Record response data.
			span.AddData("http.status", rw.statusCode)

			duration := client.clock.Now().Sub(start)
			span.AddData("http.duration_ms", duration.Milliseconds())

			// End the span with the appropriate error.
			var spanErr error
			switch {
			case panicErr != nil:
				spanErr = panicErr
			case rw.statusCode >= 500:
				spanErr = errors.New(http.StatusText(rw.statusCode))
			}

			_ = span.End(spanErr)
		})
	}
}

// statusWriter wraps an http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code and delegates to the underlying writer.
func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write ensures WriteHeader(200) is called implicitly if not yet called.
func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}
