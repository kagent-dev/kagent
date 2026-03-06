package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	fake "github.com/kagent-dev/kagent/go/core/internal/database/fake"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListPlugins_Empty(t *testing.T) {
	dbClient := fake.NewClient()
	base := &handlers.Base{DatabaseService: dbClient}
	h := handlers.NewPluginsHandler(base)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	w := newMockErrorResponseWriter()

	h.HandleListPlugins(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp api.StandardResponse[[]handlers.PluginResponse]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.Data)
}

func TestHandleListPlugins_WithPlugins(t *testing.T) {
	dbClient := fake.NewClient()
	fakeClient := dbClient.(*fake.InMemoryFakeClient)

	fakeClient.StorePlugin(&database.Plugin{
		Name:        "kagent/kanban-mcp",
		PathPrefix:  "kanban-mcp",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
		UpstreamURL: "http://kanban-mcp:8080",
	})

	base := &handlers.Base{DatabaseService: dbClient}
	h := handlers.NewPluginsHandler(base)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	w := newMockErrorResponseWriter()

	h.HandleListPlugins(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp api.StandardResponse[[]handlers.PluginResponse]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "kanban-mcp", resp.Data[0].PathPrefix)
	assert.Equal(t, "Kanban Board", resp.Data[0].DisplayName)
	assert.Equal(t, "kanban", resp.Data[0].Icon)
	assert.Equal(t, "AGENTS", resp.Data[0].Section)
}
