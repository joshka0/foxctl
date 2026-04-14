// Package main implements an HMAC authentication plugin.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	plugin "github.com/jkatigb/agentctl/internal/interfaces/openapi/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--handshake" {
		handshake := plugin.Handshake{
			Name:      "auth-hmac",
			Version:   "1.0.0",
			Commands:  []string{plugin.CommandAuth},
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
		emitRuntimeError(fmt.Errorf("decode envelope: %w", err))
		return
	}
	if env.Command != plugin.CommandAuth {
		emitRuntimeError(fmt.Errorf("unexpected command %s", env.Command))
		return
	}

	var payload plugin.AuthRequestPayload
	if err := decodePayload(env.Data, &payload); err != nil {
		emitRuntimeError(fmt.Errorf("decode payload: %w", err))
		return
	}

	// Optional delay hint for testing timeout handling.
	if delayMS, ok := payload.Context.SpecHints["delay_ms"].(float64); ok && delayMS > 0 {
		time.Sleep(time.Duration(delayMS) * time.Millisecond)
	}

	key, ok := payload.Context.Credentials["key"].(string)
	if !ok {
		emitAuthError("key credential must be a string")
		return
	}
	secret, ok := payload.Context.Credentials["secret"].(string)
	if !ok {
		emitAuthError("secret credential must be a string")
		return
	}
	if key == "" || secret == "" {
		emitAuthError("missing key or secret")
		return
	}

	dataToSign := payload.Request.Method + " " + payload.Request.URL
	if len(payload.Request.Body) > 0 {
		dataToSign += string(payload.Request.Body)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(dataToSign)); err != nil {
		emitRuntimeError(fmt.Errorf("generate signature: %w", err))
		return
	}
	signature := hex.EncodeToString(mac.Sum(nil))

	headers := map[string]string{
		"Authorization": fmt.Sprintf("HMAC %s:%s", key, signature),
	}
	result := plugin.AuthResult{Headers: headers}
	out := envelope.OK(plugin.CommandAuth, result)
	writeEnvelope(out)
}

func emitAuthError(message string) {
	data := map[string]any{
		"hint": "Provide both key and secret credentials",
	}
	env := envelope.Error(plugin.CommandAuth, "EAUTH", message, data)
	writeEnvelope(env)
}

func emitRuntimeError(err error) {
	env := envelope.Error(plugin.CommandAuth, "ERUNTIME", err.Error(), nil)
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
