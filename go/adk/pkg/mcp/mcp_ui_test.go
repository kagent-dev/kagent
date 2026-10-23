package mcp

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHasUIResourceAcceptsNestedAndFlatMetaShapes(t *testing.T) {
	t.Parallel()

	// _meta.ui.resourceUri, the nested MCP Apps shape.
	if !HasUIResource(mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://server/dashboard"}}) {
		t.Fatal("nested _meta.ui.resourceUri not recognized")
	}
	// _meta["ui/resourceUri"], the flat shape parseMCPUIMetadata also accepts.
	if !HasUIResource(mcpsdk.Meta{"ui/resourceUri": "ui://server/dashboard"}) {
		t.Fatal(`flat _meta["ui/resourceUri"] not recognized`)
	}
}

func TestHasUIResourceRejectsResultsWithoutUIResource(t *testing.T) {
	t.Parallel()

	if HasUIResource(nil) {
		t.Fatal("nil meta reported a UI resource")
	}
	if HasUIResource(mcpsdk.Meta{}) {
		t.Fatal("empty meta reported a UI resource")
	}
	if HasUIResource(mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"model"}}}) {
		t.Fatal("meta without resourceUri reported a UI resource")
	}
}
