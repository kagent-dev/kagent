// Package scheduling contains scheduling semantics shared across services.
package scheduling

// MessageIDPrefix is reserved for messages sent by a scheduler, so that a
// scheduler can find its own message by ID. No caller may use it.
const MessageIDPrefix = "scheduled-run/"
