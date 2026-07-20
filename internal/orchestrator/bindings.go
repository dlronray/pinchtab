package orchestrator

import (
	"sort"
	"sync"
	"time"
)

// Bindings tracks identity → instance mappings used to route subsequent
// unscoped requests for a given session or agent to the same instance that
// served their first successful request. Bindings are written only after a
// successful proxy and validated against running instances on read.
//
// Sessions have a lifecycle (revoke / expire / prune) that the orchestrator
// hooks into via session.Store, so sessions can be evicted promptly. Agents
// have no lifecycle signal, so agent bindings are bounded by an idle TTL and
// an LRU cap to prevent unbounded growth.
type Bindings struct {
	mu          sync.RWMutex
	session     map[string]string              // sessionID → instanceID
	sessionTabs map[string]map[string]struct{} // sessionID → observed tab IDs
	agent       map[string]string              // agentID   → instanceID
	agentSeen   map[string]time.Time           // agentID   → last access
	now         func() time.Time
}

// NewBindings returns an empty bindings table. Pass nil for `now` to use
// time.Now; tests can inject a deterministic clock.
func NewBindings(now func() time.Time) *Bindings {
	if now == nil {
		now = time.Now
	}
	return &Bindings{
		session:     make(map[string]string),
		sessionTabs: make(map[string]map[string]struct{}),
		agent:       make(map[string]string),
		agentSeen:   make(map[string]time.Time),
		now:         now,
	}
}

// ResolveSession returns the instance bound to the given session id, if any.
func (b *Bindings) ResolveSession(id string) (string, bool) {
	if b == nil || id == "" {
		return "", false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	inst, ok := b.session[id]
	return inst, ok && inst != ""
}

// ResolveAgent returns the instance bound to the given agent id, if any,
// and bumps its idle timestamp on hit.
func (b *Bindings) ResolveAgent(id string) (string, bool) {
	if b == nil || id == "" {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	inst, ok := b.agent[id]
	if !ok || inst == "" {
		return "", false
	}
	b.agentSeen[id] = b.now()
	return inst, true
}

// BindSession associates a session id with an instance. Pass an empty
// instance id to no-op; pass an empty session id to no-op.
func (b *Bindings) BindSession(id, instanceID string) {
	if b == nil || id == "" || instanceID == "" {
		return
	}
	b.mu.Lock()
	b.session[id] = instanceID
	b.mu.Unlock()
}

// BindAgent associates an agent id with an instance and updates its idle
// timestamp.
func (b *Bindings) BindAgent(id, instanceID string) {
	if b == nil || id == "" || instanceID == "" {
		return
	}
	b.mu.Lock()
	b.agent[id] = instanceID
	b.agentSeen[id] = b.now()
	b.mu.Unlock()
}

func (b *Bindings) TrackSessionTab(id, tabID string) {
	if b == nil || id == "" || tabID == "" {
		return
	}
	b.mu.Lock()
	if b.sessionTabs[id] == nil {
		b.sessionTabs[id] = make(map[string]struct{})
	}
	b.sessionTabs[id][tabID] = struct{}{}
	b.mu.Unlock()
}

func (b *Bindings) ForgetTab(tabID string) {
	if b == nil || tabID == "" {
		return
	}
	b.mu.Lock()
	for sessionID, tabs := range b.sessionTabs {
		delete(tabs, tabID)
		if len(tabs) == 0 {
			delete(b.sessionTabs, sessionID)
		}
	}
	b.mu.Unlock()
}

func (b *Bindings) SessionTabs(id string) []string {
	if b == nil || id == "" {
		return nil
	}
	b.mu.RLock()
	tabs := make([]string, 0, len(b.sessionTabs[id]))
	for tabID := range b.sessionTabs[id] {
		tabs = append(tabs, tabID)
	}
	b.mu.RUnlock()
	sort.Strings(tabs)
	return tabs
}

// ClearSession removes a session binding and its observed tab ownership. No-op if absent.
func (b *Bindings) ClearSession(id string) {
	if b == nil || id == "" {
		return
	}
	b.mu.Lock()
	delete(b.session, id)
	delete(b.sessionTabs, id)
	b.mu.Unlock()
}

// ClearAgent removes an agent binding. No-op if absent.
func (b *Bindings) ClearAgent(id string) {
	if b == nil || id == "" {
		return
	}
	b.mu.Lock()
	delete(b.agent, id)
	delete(b.agentSeen, id)
	b.mu.Unlock()
}

// ClearInstance removes every binding pointing at the given instance.
// Called from the orchestrator's instance-stopped/error handler so a
// crashed or restarted instance does not keep receiving routed traffic.
func (b *Bindings) ClearInstance(instanceID string) {
	if b == nil || instanceID == "" {
		return
	}
	b.mu.Lock()
	for id, target := range b.session {
		if target == instanceID {
			delete(b.session, id)
		}
	}
	for id, target := range b.agent {
		if target == instanceID {
			delete(b.agent, id)
			delete(b.agentSeen, id)
		}
	}
	b.mu.Unlock()
}

// PruneAgents drops agent bindings that have been idle longer than the
// given duration, then enforces an LRU cap by evicting the oldest entries
// until the count fits. A non-positive `idle` disables the TTL pass; a
// non-positive `max` disables the cap pass.
func (b *Bindings) PruneAgents(idle time.Duration, max int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if idle > 0 {
		cutoff := b.now().Add(-idle)
		for id, seen := range b.agentSeen {
			if seen.Before(cutoff) {
				delete(b.agent, id)
				delete(b.agentSeen, id)
			}
		}
	}
	if max > 0 && len(b.agent) > max {
		type entry struct {
			id   string
			seen time.Time
		}
		entries := make([]entry, 0, len(b.agentSeen))
		for id, seen := range b.agentSeen {
			entries = append(entries, entry{id, seen})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].seen.Before(entries[j].seen)
		})
		excess := len(entries) - max
		for i := 0; i < excess; i++ {
			delete(b.agent, entries[i].id)
			delete(b.agentSeen, entries[i].id)
		}
	}
}

// Counts returns the current number of session and agent bindings. Useful
// for metrics and tests.
func (b *Bindings) Counts() (sessions, agents int) {
	if b == nil {
		return 0, 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.session), len(b.agent)
}
