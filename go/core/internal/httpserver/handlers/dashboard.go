package handlers

import (
	"net/http"
	"time"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// DashboardHandler handles dashboard-related requests
type DashboardHandler struct {
	*Base
}

// NewDashboardHandler creates a new DashboardHandler
func NewDashboardHandler(base *Base) *DashboardHandler {
	return &DashboardHandler{Base: base}
}

// HandleDashboardStats handles GET /api/dashboard/stats requests
func (h *DashboardHandler) HandleDashboardStats(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("dashboard-handler").WithValues("operation", "stats")

	userID, err := GetUserID(r)
	if err != nil {
		log.V(1).Info("Failed to get user ID, using empty string for counts", "error", err)
		userID = ""
	}

	// Count agents
	agentCount := 0
	agents, err := h.DatabaseService.ListAgents()
	if err != nil {
		log.Error(err, "Failed to list agents for dashboard count")
	} else {
		agentCount = len(agents)
	}

	// Count tools
	toolCount := 0
	tools, err := h.DatabaseService.ListTools()
	if err != nil {
		log.Error(err, "Failed to list tools for dashboard count")
	} else {
		toolCount = len(tools)
	}

	// Count MCP servers (tool servers)
	mcpServerCount := 0
	toolServers, err := h.DatabaseService.ListToolServers()
	if err != nil {
		log.Error(err, "Failed to list tool servers for dashboard count")
	} else {
		mcpServerCount = len(toolServers)
	}

	counts := api.DashboardCounts{
		Agents:     agentCount,
		Tools:      toolCount,
		MCPServers: mcpServerCount,
		// K8s-only resources — will be wired to K8s list calls later
		Workflows: 0,
		CronJobs:  0,
		Models:    0,
		GitRepos:  0,
	}

	// Recent runs (sessions)
	var recentRuns []api.RecentRun
	if userID != "" {
		sessions, err := h.DatabaseService.ListSessions(userID)
		if err != nil {
			log.Error(err, "Failed to list sessions for dashboard recent runs")
		} else {
			limit := 10
			if len(sessions) < limit {
				limit = len(sessions)
			}
			recentRuns = make([]api.RecentRun, 0, limit)
			for i := 0; i < limit; i++ {
				s := sessions[i]
				sessionName := ""
				if s.Name != nil {
					sessionName = *s.Name
				}
				agentName := ""
				if s.AgentID != nil {
					agentName = *s.AgentID
				}
				recentRuns = append(recentRuns, api.RecentRun{
					SessionID:   s.ID,
					SessionName: sessionName,
					AgentName:   agentName,
					CreatedAt:   s.CreatedAt.Format(time.RFC3339),
					UpdatedAt:   s.UpdatedAt.Format(time.RFC3339),
				})
			}
		}
	}
	if recentRuns == nil {
		recentRuns = []api.RecentRun{}
	}

	// Recent events — fetching all events requires a session ID in the current API
	recentEvents := []api.RecentEvent{}

	response := api.DashboardStatsResponse{
		Counts:       counts,
		RecentRuns:   recentRuns,
		RecentEvents: recentEvents,
	}

	log.Info("Successfully retrieved dashboard stats")
	RespondWithJSON(w, http.StatusOK, response)
}
