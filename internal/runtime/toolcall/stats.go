package toolcall

import (
	"context"
	"sort"
	"sync"
)

type StatsSnapshot struct {
	TotalEvents     int64
	ByEventType     map[EventType]int64
	ByTool          map[string]int64
	ByStatus        map[Status]int64
	ByErrorCode     map[ErrorCode]int64
	DuplicateEvents int64
}

type StatsHook struct {
	mu         sync.Mutex
	total      int64
	byEvent    map[EventType]int64
	byTool     map[string]int64
	byStatus   map[Status]int64
	byError    map[ErrorCode]int64
	duplicates int64
}

func NewStatsHook() *StatsHook {
	return &StatsHook{
		byEvent:  map[EventType]int64{},
		byTool:   map[string]int64{},
		byStatus: map[Status]int64{},
		byError:  map[ErrorCode]int64{},
	}
}

func (s *StatsHook) OnToolEvent(_ context.Context, ev Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	s.byEvent[ev.Type]++
	if ev.ToolName != "" {
		s.byTool[ev.ToolName]++
	}
	if ev.Status != "" {
		s.byStatus[ev.Status]++
	}
	if ev.ErrorCode != "" {
		s.byError[ev.ErrorCode]++
	}
	if ev.Duplicate {
		s.duplicates++
	}
}

func (s *StatsHook) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return StatsSnapshot{
		TotalEvents:     s.total,
		ByEventType:     cloneEventMap(s.byEvent),
		ByTool:          cloneStringMap(s.byTool),
		ByStatus:        cloneStatusMap(s.byStatus),
		ByErrorCode:     cloneErrorMap(s.byError),
		DuplicateEvents: s.duplicates,
	}
}

func cloneEventMap(in map[EventType]int64) map[EventType]int64 {
	out := make(map[EventType]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStatusMap(in map[Status]int64) map[Status]int64 {
	out := make(map[Status]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneErrorMap(in map[ErrorCode]int64) map[ErrorCode]int64 {
	out := make(map[ErrorCode]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s StatsSnapshot) SortedToolNames() []string {
	out := make([]string, 0, len(s.ByTool))
	for k := range s.ByTool {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
