package a2a

import (
	"encoding/json"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

type ToolActivityKind string

const (
	ToolCallKind   ToolActivityKind = "function_call"
	ToolResultKind ToolActivityKind = "function_response"
)

type ToolActivity struct {
	Kind     ToolActivityKind
	ID       string
	Name     string
	Args     any
	Response any
}

func NewToolCallPart(id, name string, args any) (*a2atype.Part, error) {
	value, err := toolActivityValue(args)
	if err != nil {
		return nil, err
	}
	return newToolActivityPart(ToolCallKind, &apiv1.ToolActivity{Id: id, Name: name, Args: value})
}

func NewToolResultPart(id, name string, response any) (*a2atype.Part, error) {
	value, err := toolActivityValue(response)
	if err != nil {
		return nil, err
	}
	return newToolActivityPart(ToolResultKind, &apiv1.ToolActivity{Id: id, Name: name, Response: value})
}

func toolActivityValue(value any) (*structpb.Value, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result structpb.Value
	if err := protojson.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func ParseToolActivity(part *a2atype.Part) (ToolActivity, bool) {
	if part == nil {
		return ToolActivity{}, false
	}
	kind, ok := part.Metadata[PartTypeMetadataKey].(string)
	if !ok || (ToolActivityKind(kind) != ToolCallKind && ToolActivityKind(kind) != ToolResultKind) {
		return ToolActivity{}, false
	}
	data, ok := part.Data().(map[string]any)
	if !ok {
		return ToolActivity{}, false
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return ToolActivity{}, false
	}
	var wire apiv1.ToolActivity
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(encoded, &wire); err != nil {
		return ToolActivity{}, false
	}
	return ToolActivity{Kind: ToolActivityKind(kind), ID: wire.Id, Name: wire.Name, Args: wire.Args.AsInterface(), Response: wire.Response.AsInterface()}, true
}

func newToolActivityPart(kind ToolActivityKind, data *apiv1.ToolActivity) (*a2atype.Part, error) {
	encoded, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(data)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	part := a2atype.NewDataPart(payload)
	part.SetMeta(PartTypeMetadataKey, string(kind))
	return part, nil
}
