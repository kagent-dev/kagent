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
	"time"
)

// reapWait bounds how long End waits for a killed group's processes to exit
// so it can reap them.
const reapWait = time.Second

// Processes records the process groups one turn started.
type Processes struct {
	mu sync.Mutex
	// groups maps the ID of each group the turn started to whether the
	// command leading it is still running, whose own process os/exec reaps.
	groups map[int]bool
	ended  bool
}

type processesKey struct{}

// Begin returns a context that records the process groups started under it,
// and the Processes that End them.
func Begin(ctx context.Context) (context.Context, *Processes) {
	processes := &Processes{groups: map[int]bool{}}
	return context.WithValue(ctx, processesKey{}, processes), processes
}

// FromContext returns the Processes of the turn ctx belongs to, or nil outside
// a turn.
func FromContext(ctx context.Context) *Processes {
	processes, _ := ctx.Value(processesKey{}).(*Processes)
	return processes
}

// Track records the process group pgid, whose leading command is running, as
// started by the turn. A group started after the turn ended is killed at once.
func (p *Processes) Track(pgid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended {
		killGroup(pgid)
		return
	}
	p.groups[pgid] = true
}

// Settle records that the command leading pgid has returned. It reaps what of
// the group has exited and forgets the group once nothing of it is left, so
// End never signals a group ID the system has since given to another process.
// A group whose command returned after the turn ended is killed and reaped.
func (p *Processes) Settle(pgid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended {
		killGroup(pgid)
		reapGroup(pgid, reapWait)
		return
	}
	reapGroup(pgid, 0)
	if groupAlive(pgid) {
		p.groups[pgid] = false
	} else {
		delete(p.groups, pgid)
	}
}

// End kills every process group the turn started that is still running, and
// reaps the killed processes it is the parent of. A background job outlives
// the shell that started it and is reparented to init; when the runtime is
// init, as in a sandbox whose entrypoint it is, nothing else reaps it. A group
// whose command is still running is reaped by Settle once the command returns,
// so os/exec reaps the command itself. End is safe to call more than once.
func (p *Processes) End() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ended = true
	for pgid, running := range p.groups {
		killGroup(pgid)
		if !running {
			reapGroup(pgid, reapWait)
		}
	}
	clear(p.groups)
}
