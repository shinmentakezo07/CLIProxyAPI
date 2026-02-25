package toolcall

import "sync"

type Store interface {
	Begin(seed Record) (existing Record, duplicate bool)
	Complete(record Record)
	Get(callID string) (Record, bool)
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

func (s *MemoryStore) Begin(seed Record) (Record, bool) {
	if s == nil || seed.CallID == "" {
		return Record{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	if existing, ok := s.records[seed.CallID]; ok {
		return existing, true
	}
	s.records[seed.CallID] = seed
	return Record{}, false
}

func (s *MemoryStore) Complete(record Record) {
	if s == nil || record.CallID == "" {
		return
	}
	s.mu.Lock()
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	s.records[record.CallID] = record
	s.mu.Unlock()
}

func (s *MemoryStore) Get(callID string) (Record, bool) {
	if s == nil || callID == "" {
		return Record{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[callID]
	return rec, ok
}
