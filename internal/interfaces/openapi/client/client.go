package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/secrets"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	previewByteLimit = 512
	previewKeyLimit  = 5
)

// Client executes HTTP requests and processes responses according to the
// Core Profile rules for the http/openapi skill.
type Client struct {
	http        *http.Client
	cas         storage.CASStore
	inlineLimit int64
	maxCapture  int64
}

// Option customizes the client.
type Option func(*Client)

// WithHTTPClient overrides the underlying http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.http = hc
		}
	}
}

// New creates a new OpenAPI HTTP client using the provided configuration and
// CAS store.
func New(cfg config.Config, cas storage.CASStore, opts ...Option) *Client {
	c := &Client{
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: defaultTransport(),
		},
		cas:         cas,
		inlineLimit: int64(cfg.InlineOutputKB) * 1024,
		maxCapture:  int64(cfg.MaxCaptureKB) * 1024,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.inlineLimit <= 0 {
		c.inlineLimit = int64(config.DefaultInlineOutputKB) * 1024
	}
	if c.maxCapture <= 0 {
		c.maxCapture = int64(config.DefaultMaxCaptureKB) * 1024
	}
	if c.http == nil {
		c.http = &http.Client{
			Timeout:   30 * time.Second,
			Transport: defaultTransport(),
		}
	}
	return c
}

func defaultTransport() http.RoundTripper {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return transport
}

// Response captures the processed HTTP response.
type Response struct {
	StatusCode  int
	Headers     map[string]string
	ContentType string
	Size        int64
	Body        any
	Digest      string
	Artifact    *storage.CASObject
	Preview     Preview
	RecordCount int
	Timing      Timing
}

// Preview provides a lightweight summary of a response body.
type Preview struct {
	FirstKeys    []string `json:"first_keys,omitempty"`
	SampleRecord any      `json:"sample_record,omitempty"`
	Structure    string   `json:"structure,omitempty"`
	FirstBytes   string   `json:"first_bytes,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
}

// Timing reports latency metrics gathered during request execution.
type Timing struct {
	DNS     time.Duration
	Connect time.Duration
	TLS     time.Duration
	Total   time.Duration
}

// Error describes failures encountered while executing the request.
type Error struct {
	Code     string
	Message  string
	Err      error
	Response *Response
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "openapi client error"
}

// Unwrap exposes the underlying error for errors.Is / errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Execute performs the HTTP request and returns a processed response.
//
// Index:
//   Purpose: Execute an OpenAPI HTTP request and normalize response data
//   Flow: attach traces → send request → read body → build response metadata → inline or store in CAS
//   Related: Client.readBody, shouldInline, inlineValue, previewOnly, storage.CASStore.Put
//   Keywords: http_execute, status_code, content_type, preview, digest, timing, cas
//
// [[protocol:openapi-http-execution]]
// [[domain:http-client-boundary]]
func (c *Client) Execute(ctx context.Context, req *http.Request) (*Response, error) {
	if req == nil {
		return nil, &Error{Code: "EARG", Message: "request is nil"}
	}

	timing := Timing{}
	var dnsStart, connectStart, tlsStart time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				timing.DNS += time.Since(dnsStart)
				dnsStart = time.Time{}
			}
		},
		ConnectStart: func(_, _ string) { connectStart = time.Now() },
		ConnectDone: func(_, _ string, err error) {
			if err == nil && !connectStart.IsZero() {
				timing.Connect += time.Since(connectStart)
			}
			connectStart = time.Time{}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			if !tlsStart.IsZero() {
				timing.TLS += time.Since(tlsStart)
			}
			tlsStart = time.Time{}
		},
	}

	tracedCtx := httptrace.WithClientTrace(ctx, trace)
	req = req.Clone(tracedCtx)

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, classifyNetworkError(err)
	}
	defer resp.Body.Close()

	body, err := c.readBody(resp.Body)
	if err != nil {
		return nil, err
	}

	timing.Total = time.Since(start)

	contentType := parseContentType(resp.Header.Get("Content-Type"))
	headers := sanitizeHeaders(resp.Header)

	processed := &Response{
		StatusCode:  resp.StatusCode,
		Headers:     headers,
		ContentType: contentType,
		Size:        int64(len(body)),
		Timing:      timing,
	}

	inline := shouldInline(resp.StatusCode, len(body), c.inlineLimit)
	if inline {
		value, preview, count := inlineValue(body, contentType)
		processed.Body = value
		processed.Preview = preview
		processed.RecordCount = count
		return processed, classifyHTTPStatus(processed)
	}

	preview, count := previewOnly(body, contentType)
	processed.Preview = preview
	processed.RecordCount = count

	if c.cas == nil {
		return processed, &Error{Code: "ERUNTIME", Message: "cas store unavailable", Response: processed}
	}

	artifact, err := c.cas.Put(ctx, bytes.NewReader(body), contentType, nil)
	if err != nil {
		return processed, &Error{Code: "ERUNTIME", Err: fmt.Errorf("store response: %w", err), Response: processed}
	}
	processed.Artifact = &artifact
	processed.Digest = artifact.Digest
	processed.Size = artifact.Size
	return processed, classifyHTTPStatus(processed)
}

func (c *Client) readBody(r io.Reader) ([]byte, error) {
	if c.maxCapture <= 0 {
		c.maxCapture = int64(config.DefaultMaxCaptureKB) * 1024
	}
	limit := c.maxCapture + 1
	buf, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, &Error{Code: "ERUNTIME", Err: fmt.Errorf("read response: %w", err)}
	}
	if int64(len(buf)) >= limit {
		return nil, &Error{Code: "EOUTPUT_TOO_LARGE", Message: fmt.Sprintf("response exceeded %d bytes", c.maxCapture)}
	}
	return buf, nil
}

func classifyNetworkError(err error) *Error {
	if err == nil {
		return nil
	}
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return &Error{Code: "ECANCELED", Err: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: "ETIMEOUT", Err: err}
	case errors.As(err, &netErr):
		if netErr.Timeout() {
			return &Error{Code: "ETIMEOUT", Err: err}
		}
		return &Error{Code: "ECONNECTION", Err: err}
	default:
		return &Error{Code: "ERUNTIME", Err: err}
	}
}

func classifyHTTPStatus(resp *Response) error {
	if resp == nil {
		return nil
	}
	status := resp.StatusCode
	if status < http.StatusBadRequest {
		return nil
	}
	code := "ERUNTIME"
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		code = "EAUTH"
	case status == http.StatusNotFound:
		code = "ENOTFOUND"
	case status == http.StatusRequestTimeout:
		code = "ETIMEOUT"
	case status == http.StatusTooManyRequests:
		code = "ERATELIMIT"
	case status >= 400 && status < 500:
		code = "EARG"
	case status >= 500:
		code = "ERUNTIME"
	}
	message := http.StatusText(status)
	if message == "" {
		message = fmt.Sprintf("http status %d", status)
	} else {
		message = fmt.Sprintf("%d %s", status, message)
	}
	return &Error{Code: code, Message: message, Response: resp}
}

func sanitizeHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, values := range h {
		lower := strings.ToLower(k)
		out[lower] = strings.Join(values, ", ")
	}
	return secrets.RedactHeaders(out)
}

func parseContentType(header string) string {
	if header == "" {
		return "application/octet-stream"
	}
	parts := strings.Split(header, ";")
	if len(parts) == 0 {
		return strings.ToLower(strings.TrimSpace(header))
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func shouldInline(status int, size int, limit int64) bool {
	if status >= 400 {
		return true
	}
	if limit <= 0 {
		return true
	}
	return int64(size) <= limit
}

func inlineValue(body []byte, contentType string) (any, Preview, int) {
	if len(body) == 0 {
		return nil, Preview{}, 0
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, Preview{}, 0
	}
	if isLikelyJSON(contentType, trimmed) {
		val, preview, count := decodeJSON(trimmed)
		if val != nil {
			return val, preview, count
		}
	}
	preview := Preview{}
	str, truncated := firstBytes(trimmed)
	preview.FirstBytes = str
	preview.Truncated = truncated
	return string(trimmed), preview, 0
}

func previewOnly(body []byte, contentType string) (Preview, int) {
	if len(body) == 0 {
		return Preview{}, 0
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return Preview{}, 0
	}
	if isLikelyJSON(contentType, trimmed) {
		_, preview, count := decodeJSON(trimmed)
		return preview, count
	}
	preview := Preview{}
	str, truncated := firstBytes(trimmed)
	preview.FirstBytes = str
	preview.Truncated = truncated
	return preview, 0
}

func isLikelyJSON(contentType string, body []byte) bool {
	if strings.HasPrefix(contentType, "application/json") {
		return true
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return false
	}
	switch body[0] {
	case '{', '[', '"':
		return json.Valid(body)
	default:
		return false
	}
}

func decodeJSON(body []byte) (any, Preview, int) {
	var data any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, Preview{}, 0
	}
	preview := Preview{}
	count := 0
	switch v := data.(type) {
	case []any:
		count = len(v)
		if len(v) > 0 {
			preview.SampleRecord = sampleValue(v[0])
			preview.FirstKeys = extractKeys(v[0], previewKeyLimit)
			preview.Truncated = count > 1
		}
	case map[string]any:
		preview.Structure = "object"
		preview.FirstKeys = extractKeys(v, previewKeyLimit)
		preview.SampleRecord = sampleValue(v)
		preview.Truncated = len(v) > previewKeyLimit
	default:
		preview.SampleRecord = v
	}
	return data, preview, count
}

func sampleValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		keys := extractKeys(typed, previewKeyLimit)
		if len(keys) == 0 {
			return map[string]any{}
		}
		sample := make(map[string]any, len(keys))
		for _, k := range keys {
			sample[k] = typed[k]
		}
		return sample
	case []any:
		if len(typed) == 0 {
			return []any{}
		}
		return []any{sampleValue(typed[0])}
	default:
		return typed
	}
}

func extractKeys(v any, limit int) []string {
	var keys []string
	switch data := v.(type) {
	case map[string]any:
		keys = make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
	case []any:
		if len(data) > 0 {
			return extractKeys(data[0], limit)
		}
		return nil
	default:
		return nil
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}

func firstBytes(body []byte) (string, bool) {
	if len(body) <= previewByteLimit {
		return string(body), false
	}
	return string(body[:previewByteLimit]) + "...", true
}
