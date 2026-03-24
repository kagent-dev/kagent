package a2a

import (
	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// ---------------------------------------------------------------------------
// Part filters
// ---------------------------------------------------------------------------

// isEmptyDataPart returns true if the part is a DataPart with nil or empty Data.
// The ADK processor emits such parts as cleanup signals for streaming partial
// artifacts and as a fallback for unrecognized GenAI part types.
func isEmptyDataPart(part a2atype.Part) bool {
	dp, ok := part.(a2atype.DataPart)
	return ok && len(dp.Data) == 0
}

// filterTextParts returns only TextParts from the given parts.
func filterTextParts(parts a2atype.ContentParts) a2atype.ContentParts {
	var out a2atype.ContentParts
	for _, p := range parts {
		if _, ok := p.(a2atype.TextPart); ok {
			out = append(out, p)
		}
	}
	return out
}
