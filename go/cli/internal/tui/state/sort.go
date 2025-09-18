package state

import (
	"sort"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// SortAgentsNewestFirst sorts agents by creation timestamp descending.
func SortAgentsNewestFirst(agents []api.AgentResponse) {
	sort.Slice(agents, func(i, j int) bool {
		var ti, tj time.Time
		if agents[i].Agent != nil {
			ti = agents[i].Agent.CreationTimestamp.Time
		}
		if agents[j].Agent != nil {
			tj = agents[j].Agent.CreationTimestamp.Time
		}
		return ti.After(tj)
	})
}

// SortSessionsNewestFirst sorts sessions by UpdatedAt then CreatedAt descending.
func SortSessionsNewestFirst(sessions []*api.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		ui := sessions[i].UpdatedAt
		uj := sessions[j].UpdatedAt
		if !ui.Equal(uj) {
			return ui.After(uj)
		}
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})
}
