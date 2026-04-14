// Package actor provides a reactive actor system for agentctl agents.
//
// The actor system transforms poll-based agent daemons into event-driven actors
// that react to messages as they arrive. A supervisor manages actor lifecycles
// and routes messages using SQLite-based notifications.
//
// See docs/designs/reactive-actor-system.md for the full design.
package actor
