/*
Copyright 2025.

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

package workflow

import (
	"context"
	"sync"
)

// ActionHandler is the interface that action implementations must satisfy.
type ActionHandler interface {
	Execute(ctx context.Context, inputs map[string]string) (*ActionResult, error)
}

// ActionHandlerFunc is an adapter to allow use of ordinary functions as ActionHandlers.
type ActionHandlerFunc func(ctx context.Context, inputs map[string]string) (*ActionResult, error)

// Execute calls f(ctx, inputs).
func (f ActionHandlerFunc) Execute(ctx context.Context, inputs map[string]string) (*ActionResult, error) {
	return f(ctx, inputs)
}

// ActionRegistry holds named action handlers.
type ActionRegistry struct {
	mu       sync.RWMutex
	handlers map[string]ActionHandler
}

// NewActionRegistry creates a new empty ActionRegistry.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{
		handlers: make(map[string]ActionHandler),
	}
}

// Register adds a handler for the given action name.
func (r *ActionRegistry) Register(name string, handler ActionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
}

// Get returns the handler for the given action name.
func (r *ActionRegistry) Get(name string) (ActionHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}
