// Package skills exposes embedded skill assets for the CLI.
package skills

import _ "embed"

// WasiEchoManifest is the embedded skill.yaml for the wasi/echo sample.
//
//go:embed wasi_echo/skill.yaml
var WasiEchoManifest []byte

// WasiEchoModule is the embedded wasm module for the wasi/echo sample.
//
//go:embed wasi_echo/module.wasm
var WasiEchoModule []byte
