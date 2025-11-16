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
	plugin "github.com/jkatigb/agentctl/internal/openapi/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--handshake" {
		handshake := plugin.Handshake{
			Name:      "auth-hmac",
			Version:   "1.0.0",
			Commands:  []string{plugin.CommandAuth},
			Protocols: []string{"core/v1"},
		}
		_ = json.NewEncoder(os.Stdout).Encode(handshake)
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

	key, _ := payload.Context.Credentials["key"].(string)
	secret, _ := payload.Context.Credentials["secret"].(string)
	if key == "" || secret == "" {
		emitAuthError("missing key or secret")
		return
	}

	dataToSign := payload.Request.Method + " " + payload.Request.URL
	if len(payload.Request.Body) > 0 {
		dataToSign += string(payload.Request.Body)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(dataToSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	headers := map[string]string{
		"Authorization": fmt.Sprintf("HMAC %s:%s", key, signature),
	}
	result := plugin.AuthResult{Headers: headers}
	out := envelope.OK(plugin.CommandAuth, result)
	_ = envelope.Write(os.Stdout, out)
}

func emitAuthError(message string) {
	data := map[string]any{
		"hint": "Provide both key and secret credentials",
	}
	env := envelope.Error(plugin.CommandAuth, "EAUTH", message, data)
	_ = envelope.Write(os.Stdout, env)
}

func emitRuntimeError(err error) {
	env := envelope.Error(plugin.CommandAuth, "ERUNTIME", err.Error(), nil)
	_ = envelope.Write(os.Stdout, env)
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
