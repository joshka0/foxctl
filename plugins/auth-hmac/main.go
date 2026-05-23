// Package main implements an HMAC authentication plugin.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	plugin "github.com/joshka0/foxctl/internal/interfaces/openapi/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--handshake" {
		handshake := plugin.Handshake{
			Name:      "auth-hmac",
			Version:   "1.0.0",
			Commands:  []string{plugin.CommandAuth},
			Protocols: []string{"core/v1"},
		}
		if err := plugin.WriteHandshake(os.Stdout, handshake); err != nil {
			safeStderr("write handshake: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var payload plugin.AuthRequestPayload
	if err := plugin.ReadRequest(os.Stdin, plugin.CommandAuth, &payload); err != nil {
		emitRuntimeError(err)
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
	if err := plugin.WriteOK(os.Stdout, plugin.CommandAuth, result); err != nil {
		safeStderr("write envelope: %v\n", err)
	}
}

func emitAuthError(message string) {
	data := map[string]any{
		"hint": "Provide both key and secret credentials",
	}
	if err := plugin.WriteError(os.Stdout, plugin.CommandAuth, "EAUTH", message, data); err != nil {
		safeStderr("write envelope: %v\n", err)
	}
}

func emitRuntimeError(err error) {
	if err := plugin.WriteError(os.Stdout, plugin.CommandAuth, "ERUNTIME", err.Error(), nil); err != nil {
		safeStderr("write envelope: %v\n", err)
	}
}

func safeStderr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
