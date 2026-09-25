package a2a

import a2atype "github.com/a2aproject/a2a-go/v2/a2a"

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

func NewToolCallPart(id, name string, args any) *a2atype.Part {
	return newToolActivityPart(ToolCallKind, map[string]any{"id": id, "name": name, "args": args})
}

func NewToolResultPart(id, name string, response any) *a2atype.Part {
	return newToolActivityPart(ToolResultKind, map[string]any{"id": id, "name": name, "response": response})
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
	activity := ToolActivity{Kind: ToolActivityKind(kind)}
	activity.ID, _ = data["id"].(string)
	activity.Name, _ = data["name"].(string)
	activity.Args = data["args"]
	activity.Response = data["response"]
	return activity, true
}

func newToolActivityPart(kind ToolActivityKind, data map[string]any) *a2atype.Part {
	part := a2atype.NewDataPart(data)
	part.SetMeta(PartTypeMetadataKey, string(kind))
	return part
}
