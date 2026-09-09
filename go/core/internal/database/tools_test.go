package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
)

func TestToolCatalogLifecycle(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	connected := time.Now().UTC().Truncate(time.Microsecond)
	server := &ToolServer{Name: "shared", GroupKind: "remote", Description: "original", LastConnected: &connected}
	tool := &v1alpha3.MCPTool{Name: "tool", Description: "original"}
	require.NoError(t, client.RefreshToolServer(ctx, server, tool))
	originalServers, err := client.ListToolServers(ctx)
	require.NoError(t, err)
	originalTools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, originalServers, 1)
	require.Len(t, originalTools, 1)
	require.True(t, connected.Equal(*originalServers[0].LastConnected))

	// A same-name server of another kind is a separate catalog.
	require.NoError(t, client.RefreshToolServer(ctx, &ToolServer{Name: "shared", GroupKind: "local"}, &v1alpha3.MCPTool{Name: "other"}))
	require.NoError(t, client.DeleteToolServer(ctx, "shared", "remote"))
	require.NoError(t, client.DeleteToolServer(ctx, "shared", "remote"))
	require.NoError(t, client.DeleteToolServer(ctx, "missing", "remote"))
	servers, err := client.ListToolServers(ctx)
	require.NoError(t, err)
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, "local", servers[0].GroupKind)
	require.Len(t, tools, 1)
	require.Equal(t, "other", tools[0].ID)

	server.Description, server.LastConnected = "updated", nil
	tool.Description = "updated"
	require.NoError(t, client.RefreshToolServer(ctx, server, tool))
	require.NoError(t, client.RefreshToolServer(ctx, server, tool))
	servers, err = client.ListToolServers(ctx)
	require.NoError(t, err)
	tools, err = client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 2)
	require.Len(t, tools, 2)
	require.Equal(t, originalServers[0].CreatedAt, servers[0].CreatedAt)
	require.Equal(t, originalTools[0].CreatedAt, tools[0].CreatedAt)
	require.Equal(t, "updated", servers[0].Description)
	require.Equal(t, "updated", tools[0].Description)
	require.Nil(t, servers[0].LastConnected)
	require.Nil(t, servers[0].DeletedAt)
	require.Nil(t, tools[0].DeletedAt)

	require.NoError(t, client.RefreshToolServer(ctx, server))
	servers, err = client.ListToolServers(ctx)
	require.NoError(t, err)
	tools, err = client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 2)
	require.Len(t, tools, 1)
	require.Equal(t, "other", tools[0].ID)
}

func TestConcurrentToolCatalogReplacement(t *testing.T) {
	for _, initial := range []string{"missing", "empty", "deleted"} {
		t.Run(initial, func(t *testing.T) {
			client := NewClient(setupTestDB(t))
			ctx := t.Context()
			if initial != "missing" {
				require.NoError(t, client.RefreshToolServer(ctx, &ToolServer{Name: "server", GroupKind: "kind"}))
			}
			if initial == "deleted" {
				require.NoError(t, client.DeleteToolServer(ctx, "server", "kind"))
			}
			for _, withDeletes := range []bool{false, true} {
				start := make(chan struct{})
				errs := make(chan error, 12)
				for i := range 12 {
					go func() {
						<-start
						if withDeletes && i%3 == 0 {
							errs <- client.DeleteToolServer(ctx, "server", "kind")
							return
						}
						description := fmt.Sprint(i)
						errs <- client.RefreshToolServer(ctx, &ToolServer{Name: "server", GroupKind: "kind", Description: description},
							&v1alpha3.MCPTool{Name: "a-" + description, Description: description},
							&v1alpha3.MCPTool{Name: "b-" + description, Description: description})
					}()
				}
				close(start)
				for range 12 {
					require.NoError(t, <-errs)
				}
				servers, err := client.ListToolServers(ctx)
				require.NoError(t, err)
				tools, err := client.ListTools(ctx)
				require.NoError(t, err)
				if withDeletes && len(servers) == 0 {
					require.Empty(t, tools)
					continue
				}
				require.Len(t, servers, 1)
				require.Len(t, tools, 2, "replacement must not leave a union of concurrent catalogs")
				for _, tool := range tools {
					require.Equal(t, servers[0].Description, tool.Description)
				}
			}
		})
	}
}

func TestToolCatalogWritesRollbackTogether(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := t.Context()
	server := &ToolServer{Name: "server", GroupKind: "kind", Description: "original"}
	require.NoError(t, client.RefreshToolServer(ctx, server, &v1alpha3.MCPTool{Name: "original"}))
	originalServers, err := client.ListToolServers(ctx)
	require.NoError(t, err)
	originalTools, err := client.ListTools(ctx)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `ALTER TABLE tool ADD CONSTRAINT reject_bad_description CHECK (description <> 'reject')`)
	require.NoError(t, err)
	server.Description = "changed"
	require.Error(t, client.RefreshToolServer(ctx, server, &v1alpha3.MCPTool{Name: "new", Description: "reject"}))
	servers, err := client.ListToolServers(ctx)
	require.NoError(t, err)
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Equal(t, originalServers, servers)
	require.Equal(t, originalTools, tools)

	_, err = db.Exec(ctx, `ALTER TABLE tool ADD CONSTRAINT reject_delete CHECK (deleted_at IS NULL)`)
	require.NoError(t, err)
	require.Error(t, client.DeleteToolServer(ctx, server.Name, server.GroupKind))
	servers, err = client.ListToolServers(ctx)
	require.NoError(t, err)
	tools, err = client.ListTools(ctx)
	require.NoError(t, err)
	require.Equal(t, originalServers, servers)
	require.Equal(t, originalTools, tools)
}
