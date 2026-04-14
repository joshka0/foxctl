// Package gopls provides a persistent gopls daemon for fast LSP operations.
// Instead of spawning a new gopls process for each request (~3-5s startup),
// this package maintains a single long-running gopls process and communicates
// via JSON-RPC over stdio.
package gopls
