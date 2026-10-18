package translator

import (
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// RuntimeMCPHeaders excludes discovery credentials before any value resolution,
// including Secret environment placeholders and gateway credential bindings.
func RuntimeMCPHeaders(server *v1alpha3.RemoteMCPServer) []v1alpha3.ValueRef {
	if server.Spec.TokenPassthrough == nil || !*server.Spec.TokenPassthrough {
		return server.Spec.HeadersFrom
	}
	refs := make([]v1alpha3.ValueRef, 0, len(server.Spec.HeadersFrom))
	for _, ref := range server.Spec.HeadersFrom {
		if !strings.EqualFold(ref.Name, "Authorization") {
			refs = append(refs, ref)
		}
	}
	return refs
}
