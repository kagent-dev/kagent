// Package egresspolicy lets an embedding controller extend actor EgressPolicies.
package egresspolicy

import (
	"context"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
)

// GatewayTarget is the header a tokenPassthrough client sends the caller's
// token in. Origins are canonical (https://host:443) and headers lowercase.
type GatewayTarget struct {
	Origin string `json:"origin"`
	Header string `json:"header"`
	Prefix string `json:"prefix,omitempty"`
}

// Input is the persisted state a Compiler reads for one actor.
type Input struct {
	Atespace       string
	GatewayTargets []GatewayTarget
}

// Compiler adds a ReplaceHeaders entry with the target's header, prefix and an
// ate-secret credential_uri. It must not change rules or existing headers.
// Compilers run in order; together they must bind every target.
type Compiler interface {
	Compile(context.Context, Input, *ateapipb.EgressPolicy) error
}
