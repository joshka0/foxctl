// Package eino provides a config-gated adapter that bridges Eino's adk.Agent
// interface to foxctl's engine.AgentEngine interface.
//
// This adapter is a spike — it exists behind the FOXCTL_ENGINE_BACKEND env
// gate and must not affect the default mailbox-owned runtime path established
// in Milestone 1. When the gate is unset or set to any value other than "eino",
// the adapter is never instantiated and the LLMChatEngine is used unchanged.
package eino
