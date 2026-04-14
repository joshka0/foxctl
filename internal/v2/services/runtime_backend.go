package services

import coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"

// RuntimeSpawner creates one child worker from a canonical spawn request.
type RuntimeSpawner = coreworker.Spawner

// RuntimeStateReader exposes worker state for runtime trees and status views.
type RuntimeStateReader = coreworker.StateReader

// RuntimeSignaler delivers runtime signals to workers.
type RuntimeSignaler = coreworker.Signaler
