package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ConsoleEventStreamOptions configures the stream endpoint shape.
type ConsoleEventStreamOptions struct {
	PayloadFormat bool
}

// ReadConsoleEventStream reads a console SSE stream and invokes onEvent for each parsed event.
func ReadConsoleEventStream(
	ctx context.Context,
	client *APIClient,
	sessionID string,
	opts ConsoleEventStreamOptions,
	onEvent func(ConsoleStreamEvent) error,
) error {
	if client == nil {
		return errors.New("api client is required")
	}
	if client.httpClient == nil {
		return errors.New("api http client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint, err := ConsoleSessionEventsEndpoint(sessionID, opts.PayloadFormat)
	if err != nil {
		return err
	}

	requestURL, err := client.endpointURL(endpoint)
	if err != nil {
		return fmt.Errorf("build stream endpoint url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", http.MethodGet, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAPIErrorBodyBytes))
		if readErr != nil {
			return fmt.Errorf("read error response: %w", readErr)
		}
		return &HTTPStatusError{
			Method:     http.MethodGet,
			URL:        requestURL,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(raw)),
		}
	}

	return ParseConsoleEventStream(resp.Body, onEvent)
}
