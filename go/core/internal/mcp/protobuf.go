package mcp

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"buf.build/go/protovalidate"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

//go:embed schema/*.json
var toolSchemas embed.FS

var toolJSON = protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}

func addProtoTool[In, Out proto.Message](server *mcp.Server, tool *mcp.Tool, newInput func() In, newOutput func() Out,
	handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) error {
	input, err := messageSchema(newInput())
	if err != nil {
		return err
	}
	output, err := messageSchema(newOutput())
	if err != nil {
		return err
	}
	tool.InputSchema, tool.OutputSchema = input, output
	mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, raw json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
		input := newInput()
		if err := protojson.Unmarshal(raw, input); err != nil {
			return nil, nil, err
		}
		if err := protovalidate.Validate(input); err != nil {
			return nil, nil, err
		}
		result, output, err := handler(ctx, request, input)
		if err != nil {
			return result, nil, err
		}
		encoded, err := toolJSON.Marshal(output)
		return result, json.RawMessage(encoded), err
	})
	return nil
}

func messageSchema(message proto.Message) (*jsonschema.Schema, error) {
	name := string(message.ProtoReflect().Descriptor().FullName())
	data, err := toolSchemas.ReadFile("schema/" + name + ".jsonschema.bundle.json")
	if err != nil {
		return nil, fmt.Errorf("load %s schema: %w", name, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode %s schema: %w", name, err)
	}
	schema.ID = ""
	schema.Type = "object"
	return &schema, nil
}
