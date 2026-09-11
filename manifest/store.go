package manifest

import "sync/atomic"

// Store exposes immutable semantic manifests. Readers never lock; reload builds
// a complete manifest first and atomically swaps the pointer.
type Store struct {
	current atomic.Pointer[semanticSnapshot]
}

type semanticSnapshot struct {
	manifest *SemanticManifest
	graph    *SemanticGraph
}

func NewStore(initial *SemanticManifest) *Store {
	s := &Store{}
	if initial != nil {
		s.Swap(initial)
	}
	return s
}
func (s *Store) Current() *SemanticManifest {
	if snapshot := s.current.Load(); snapshot != nil {
		return snapshot.manifest
	}
	return nil
}

// Lookup returns one manifest and graph snapshot, guaranteeing both derive
// from the same manifest digest even while reloads occur concurrently.
func (s *Store) Lookup() (*SemanticManifestLookup, error) {
	if s == nil {
		return nil, nil
	}
	snapshot := s.current.Load()
	if snapshot == nil || snapshot.manifest == nil {
		return nil, nil
	}
	return NewSemanticManifestLookup(snapshot.manifest, snapshot.graph)
}

func (s *Store) Swap(next *SemanticManifest) {
	if next != nil {
		graph, err := BuildSemanticGraph(next)
		if err != nil {
			return
		}
		s.current.Store(&semanticSnapshot{manifest: next, graph: graph})
	}
}
