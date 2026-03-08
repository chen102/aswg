package adapter

import (
	"sort"
	"sync"

	"agent-session-web-gateway/backend/internal/model"
)

type Registry struct {
	mu             sync.RWMutex
	adapters       map[string]AgentAdapter
	defaultAdapter string
}

func NewRegistry(defaultAdapter string) *Registry {
	return &Registry{
		adapters:       make(map[string]AgentAdapter),
		defaultAdapter: defaultAdapter,
	}
}

func (r *Registry) Register(a AgentAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

func (r *Registry) Get(name string) (AgentAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

func (r *Registry) List() []model.AdapterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]model.AdapterInfo, 0, len(r.adapters))
	for _, a := range r.adapters {
		items = append(items, model.AdapterInfo{
			Name:         a.Name(),
			DisplayName:  a.DisplayName(),
			Enabled:      true,
			Default:      a.Name() == r.defaultAdapter,
			Capabilities: a.Capabilities(),
			Version:      a.Version(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (r *Registry) DefaultAdapter() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultAdapter
}
