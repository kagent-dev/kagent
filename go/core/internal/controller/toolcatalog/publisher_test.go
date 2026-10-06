/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package toolcatalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/toolcatalog"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

type recordingStore struct {
	writes int
	err    error
}

var _ toolcatalog.Store = (*recordingStore)(nil)

func (s *recordingStore) RefreshToolServer(context.Context, *database.ToolServer, ...*v1alpha3.MCPTool) error {
	s.writes++
	return s.err
}

func (s *recordingStore) DeleteToolServer(context.Context, string, string) error {
	return s.err
}

func TestPublisherPersistsCatalogChanges(t *testing.T) {
	store := &recordingStore{}
	publisher := toolcatalog.NewPublisher(store)
	for _, test := range []struct {
		name, description string
		uid               types.UID
		connected         bool
		tools             []*v1alpha3.MCPTool
		writes            int
	}{
		{name: "first observation", uid: "first", connected: true, tools: []*v1alpha3.MCPTool{{Name: "tool"}}, writes: 1},
		{name: "unchanged discovery", uid: "first", connected: true, tools: []*v1alpha3.MCPTool{{Name: "tool"}}, writes: 1},
		{name: "server description", uid: "first", description: "edited", connected: true, tools: []*v1alpha3.MCPTool{{Name: "tool"}}, writes: 2},
		{name: "tool description", uid: "first", description: "edited", connected: true, tools: []*v1alpha3.MCPTool{{Name: "tool", Description: "edited"}}, writes: 3},
		{name: "removed tools", uid: "first", description: "edited", connected: true, tools: []*v1alpha3.MCPTool{}, writes: 4},
		{name: "disconnected", uid: "first", description: "edited", writes: 5},
		{name: "still disconnected", uid: "first", description: "edited", writes: 5},
		{name: "edit while disconnected", uid: "first", description: "offline edit", writes: 6},
		{name: "recovered empty catalog", uid: "first", description: "offline edit", connected: true, tools: []*v1alpha3.MCPTool{}, writes: 7},
		{name: "recreated resource", uid: "replacement", description: "offline edit", connected: true, tools: []*v1alpha3.MCPTool{}, writes: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &database.ToolServer{Name: "ns/server", GroupKind: "kind", Description: test.description}
			if test.connected {
				server.LastConnected = new(time.Now())
			}
			require.NoError(t, publisher.Refresh(t.Context(), test.uid, server, test.tools...))
			require.Equal(t, test.writes, store.writes)
		})
	}
}

func TestPublisherRetriesAfterUncertainWriteOrDeletion(t *testing.T) {
	for _, operation := range []string{"refresh", "delete"} {
		t.Run(operation, func(t *testing.T) {
			store := &recordingStore{}
			publisher := toolcatalog.NewPublisher(store)
			original := &database.ToolServer{Name: "ns/server", GroupKind: "kind"}
			require.NoError(t, publisher.Refresh(t.Context(), "uid", original))
			store.err = errors.New("uncertain database result")
			if operation == "refresh" {
				edited := *original
				edited.Description = "changed"
				require.ErrorIs(t, publisher.Refresh(t.Context(), "uid", &edited), store.err)
			} else {
				require.ErrorIs(t, publisher.Delete(t.Context(), original.Name, original.GroupKind), store.err)
			}
			before := store.writes
			store.err = nil
			require.NoError(t, publisher.Refresh(t.Context(), "uid", original))
			require.Equal(t, before+1, store.writes, "returning to the old catalog must retry persistence")
			require.NoError(t, publisher.Refresh(t.Context(), "uid", original))
			require.Equal(t, before+1, store.writes, "only a successful write may suppress the next one")
		})
	}
}

func TestPublisherRepublishesAfterDeletionAndRestart(t *testing.T) {
	store := &recordingStore{}
	publisher := toolcatalog.NewPublisher(store)
	server := &database.ToolServer{Name: "ns/server", GroupKind: "kind"}
	require.NoError(t, publisher.Refresh(t.Context(), "uid", server))
	require.NoError(t, publisher.Delete(t.Context(), server.Name, server.GroupKind))
	require.NoError(t, publisher.Refresh(t.Context(), "uid", server))
	require.Equal(t, 2, store.writes)

	restarted := toolcatalog.NewPublisher(store)
	require.NoError(t, restarted.Refresh(t.Context(), "uid", server))
	require.Equal(t, 3, store.writes)
}

func TestPublisherSeparatesServerIdentities(t *testing.T) {
	store := &recordingStore{}
	publisher := toolcatalog.NewPublisher(store)
	for _, server := range []*database.ToolServer{
		{Name: "ns/server", GroupKind: "remote"},
		{Name: "ns/server", GroupKind: "local"},
		{Name: "other/server", GroupKind: "remote"},
	} {
		require.NoError(t, publisher.Refresh(t.Context(), "uid", server))
		require.NoError(t, publisher.Refresh(t.Context(), "uid", server))
	}
	require.Equal(t, 3, store.writes)
}
