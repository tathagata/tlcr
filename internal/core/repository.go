package core

import "sync"

// Repository owns immutable analysis snapshots. Refresh is explicit, never a navigation side effect.
type Repository struct {
	index   *Index
	graph   *Graph
	mu      sync.RWMutex
	refresh sync.Mutex
}

// NewRepository wraps a scanned repository for reuse by any surface.
func NewRepository(idx *Index) *Repository { return &Repository{index: idx, graph: BuildGraph(idx)} }

// Snapshot returns the current immutable index. Callers must not mutate it.
func (r *Repository) Snapshot() *Index { r.mu.RLock(); defer r.mu.RUnlock(); return r.index }

// View returns a matching immutable index and graph pair.
func (r *Repository) View() (*Index, *Graph) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.index, r.graph
}

// Refresh replaces the snapshot atomically only after successful analysis.
func (r *Repository) Refresh() error {
	r.refresh.Lock()
	defer r.refresh.Unlock()
	idx, err := ScanWithRegistry(r.Snapshot().Root, r.Snapshot().registry)
	if err != nil {
		return err
	}
	graph := BuildGraph(idx)
	r.mu.Lock()
	r.index = idx
	r.graph = graph
	r.mu.Unlock()
	return nil
}
