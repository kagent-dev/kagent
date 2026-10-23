package substrate

import (
	"encoding/base64"
	"encoding/json"
)

// These tokens match Substrate v0.7.0-alpha1's private atepg/pagetoken.go
// format. Keep this adapter and its contract tests aligned when upgrading.
// Callers use them to resume after a returned identity rather than expose a
// backend cursor that can name a filtered-out row.
type pageToken struct {
	Version int      `json:"v"`
	Kind    string   `json:"kind"`
	Scope   string   `json:"scope"`
	Last    []string `json:"last"`
}

// ActorPageToken resumes a scoped or global actor query after the given identity.
func ActorPageToken(scope, atespace, name string) string {
	last := []string{name}
	if scope == "" {
		last = []string{atespace, name}
	}
	return encodePageToken(pageToken{Version: 1, Kind: "actor", Scope: scope, Last: last})
}

// WorkerPageToken resumes the globally name-ordered worker query.
func WorkerPageToken(name string) string {
	return encodePageToken(pageToken{Version: 1, Kind: "worker", Last: []string{name}})
}

func encodePageToken(token pageToken) string {
	// This fixed struct contains only JSON-supported scalar and slice types.
	raw, _ := json.Marshal(token)
	return base64.StdEncoding.EncodeToString(raw)
}
