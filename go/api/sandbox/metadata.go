// Package sandbox defines routing metadata for sandbox execution APIs.
package sandbox

// IDHeader selects the Sandbox UUID for an upstream env process or file RPC.
// Supply exactly one value per RPC; authentication is supplied separately.
const IDHeader = "kagent-sandbox-id"
