// Package config loads, validates, and adapts ADK runtime configuration.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// Marshal validates compiled runtime input before publishing it to an actor.
func Marshal(cfg *configv1.ADKConfig) ([]byte, error) {
	if err := protovalidate.Validate(cfg); err != nil {
		return nil, fmt.Errorf("failed to validate ADK config: %w", err)
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ADK config: %w", err)
	}
	// ProtoJSON permits variable whitespace. Revisions hash these bytes.
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("failed to compact ADK config: %w", err)
	}
	return compact.Bytes(), nil
}

// Unmarshal accepts only the protobuf contract and leaves cfg intact on failure.
func Unmarshal(data []byte, cfg *configv1.ADKConfig) error {
	parsed := new(configv1.ADKConfig)
	if err := protojson.Unmarshal(data, parsed); err != nil {
		return fmt.Errorf("failed to parse ADK config: %w", err)
	}
	if err := protovalidate.Validate(parsed); err != nil {
		return fmt.Errorf("failed to validate ADK config: %w", err)
	}
	proto.Reset(cfg)
	proto.Merge(cfg, parsed)
	return nil
}
