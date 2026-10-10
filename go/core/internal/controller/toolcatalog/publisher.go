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

package toolcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"k8s.io/apimachinery/pkg/types"
)

// Store atomically persists each server and its visible tools.
type Store interface {
	RefreshToolServer(context.Context, *database.ToolServer, ...*v1alpha3.MCPTool) error
	DeleteToolServer(context.Context, string, string) error
}

// Publisher skips unchanged catalog writes within one controller process.
// Controller queues serialize operations for the same server; different servers
// may be reconciled concurrently. A new process always publishes its first observation.
type Publisher struct {
	store     Store
	snapshots sync.Map
}

type serverKey struct {
	name, groupKind string
}

func NewPublisher(store Store) *Publisher {
	return &Publisher{store: store}
}

// Refresh expects tools ordered by NormalizeTools. LastConnected records the
// last published connected observation, rather than every successful discovery.
func (p *Publisher) Refresh(ctx context.Context, uid types.UID, server *database.ToolServer, tools ...*v1alpha3.MCPTool) error {
	key := serverKey{server.Name, server.GroupKind}
	data, err := json.Marshal(struct {
		UID         types.UID
		Description string
		Connected   bool
		Tools       []*v1alpha3.MCPTool
	}{uid, server.Description, server.LastConnected != nil, tools})
	if err != nil {
		return fmt.Errorf("encode tool catalog snapshot: %w", err)
	}
	snapshot := sha256.Sum256(data)
	if previous, ok := p.snapshots.Load(key); ok && previous == snapshot {
		return nil
	}
	// An uncertain write must invalidate the old observation too: reverting
	// to that catalog later must retry persistence rather than trust the cache.
	p.snapshots.Delete(key)
	if err := p.store.RefreshToolServer(ctx, server, tools...); err != nil {
		return err
	}
	p.snapshots.Store(key, snapshot)
	return nil
}

func (p *Publisher) Delete(ctx context.Context, name, groupKind string) error {
	p.snapshots.Delete(serverKey{name, groupKind})
	return p.store.DeleteToolServer(ctx, name, groupKind)
}
