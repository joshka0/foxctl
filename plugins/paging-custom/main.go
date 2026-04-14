// Package main implements a custom pagination plugin.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	plugin "github.com/jkatigb/agentctl/internal/interfaces/openapi/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--handshake" {
		handshake := plugin.Handshake{
			Name:      "paging-custom",
			Version:   "1.0.0",
			Commands:  []string{plugin.CommandPagination},
			Protocols: []string{"core/v1"},
		}
		if err := json.NewEncoder(os.Stdout).Encode(handshake); err != nil {
			safeStderr("write handshake: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var env envelope.Envelope
	if err := json.NewDecoder(os.Stdin).Decode(&env); err != nil {
		emitError(fmt.Errorf("decode envelope: %w", err))
		return
	}
	if env.Command != plugin.CommandPagination {
		emitError(fmt.Errorf("unexpected command %s", env.Command))
		return
	}

	var payload plugin.PaginationRequestPayload
	if err := decodePayload(env.Data, &payload); err != nil {
		emitError(fmt.Errorf("decode payload: %w", err))
		return
	}

	var body map[string]any
	if len(payload.LastResponse.Body) > 0 {
		if err := json.Unmarshal(payload.LastResponse.Body, &body); err != nil {
			emitError(fmt.Errorf("decode last response: %w", err))
			return
		}
	}

	cursor := extractCursor(body)
	items := extractItems(body)
	total := payload.ItemsFetchedSoFar + len(items)

	result := plugin.PaginationResult{
		Continue:    cursor != "",
		ItemsInPage: len(items),
	}
	if cursor != "" {
		result.NextQuery = map[string]string{"cursor": cursor}
		result.NextCursor = cursor
	}
	if maxItems, ok := payload.Context.SpecHints["max_items"].(float64); ok && maxItems > 0 {
		if float64(total) >= maxItems {
			result.Continue = false
		}
	}

	out := envelope.OK(plugin.CommandPagination, result)
	writeEnvelope(out)
}

func extractCursor(body map[string]any) string {
	if body == nil {
		return ""
	}
	if meta, ok := body["meta"].(map[string]any); ok {
		if next, ok := meta["next_cursor"].(string); ok {
			return next
		}
		if next, ok := meta["next"].(string); ok {
			return next
		}
	}
	if next, ok := body["next"].(string); ok {
		return next
	}
	return ""
}

func extractItems(body map[string]any) []any {
	if body == nil {
		return nil
	}
	if items, ok := body["items"].([]any); ok {
		return items
	}
	if results, ok := body["results"].([]any); ok {
		return results
	}
	return nil
}

func emitError(err error) {
	env := envelope.Error(plugin.CommandPagination, "ERUNTIME", err.Error(), nil)
	writeEnvelope(env)
}

func writeEnvelope(env envelope.Envelope) {
	if err := envelope.Write(os.Stdout, env); err != nil {
		safeStderr("write envelope: %v\n", err)
	}
}

func safeStderr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func decodePayload(data any, v any) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
