// Package turn bounds what a tool starts by the A2A turn that started it.
//
// A turn runs with the caller's identity: its bearer token, and in a Session
// the credentials the egress gateway attaches while that caller's turn runs. A
// process a tool leaves running in the background would carry on into the next
// turn, which may belong to another caller, so every process group a turn
// started is killed when the turn ends.
package turn

import (
	"context"
	"sync"
)

// Processes records the process groups one turn started.
type Processes struct {
	mu     sync.Mutex
	groups map[int]struct{}
	ended  bool
}

type processesKey struct{}

// Begin returns a context that records the process groups started under it,
// and the Processes that End them.
func Begin(ctx context.Context) (context.Context, *Processes) {
	processes := &Processes{groups: map[int]struct{}{}}
	return context.WithValue(ctx, processesKey{}, processes), processes
}

// FromContext returns the Processes of the turn ctx belongs to, or nil outside
// a turn.
func FromContext(ctx context.Context) *Processes {
	processes, _ := ctx.Value(processesKey{}).(*Processes)
	return processes
}

// Track records the process group pgid as started by the turn. A group started
// after the turn ended is killed at once.
func (p *Processes) Track(pgid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended {
		killGroup(pgid)
		return
	}
	p.groups[pgid] = struct{}{}
}

// Settle forgets pgid once none of its processes is left, so End never signals
// a group ID the system has since given to an unrelated process.
func (p *Processes) Settle(pgid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !groupAlive(pgid) {
		delete(p.groups, pgid)
	}
}

// End kills every process group the turn started that is still running. It is
// safe to call more than once.
func (p *Processes) End() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ended = true
	for pgid := range p.groups {
		killGroup(pgid)
	}
	clear(p.groups)
}
