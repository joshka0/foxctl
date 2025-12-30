//go:build !cgo

package buildinfo

// isCGO is false when the binary is built without CGO.
const isCGO = false
