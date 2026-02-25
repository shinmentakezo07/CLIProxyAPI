package toolcall

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu   sync.RWMutex
	defs map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]Definition)}
}

func (r *Registry) Register(def Definition) error {
	if r == nil {
		return fmt.Errorf("toolcall registry is nil")
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if def.Handler == nil {
		return fmt.Errorf("tool %q handler is required", name)
	}
	def.Name = name

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defs == nil {
		r.defs = make(map[string]Definition)
	}
	if _, exists := r.defs[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.defs[name] = def
	return nil
}

func (r *Registry) MustRegister(def Definition) {
	if err := r.Register(def); err != nil {
		panic(err)
	}
}

func (r *Registry) Get(name string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[strings.TrimSpace(name)]
	return def, ok
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.defs))
	for name := range r.defs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
