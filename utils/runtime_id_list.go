package utils

import (
	"errors"
	"log/slog"
	"sync"
)

// ActiveAgentRuntimeIDList is a thread-safe linked list of active runtime IDs.
type ActiveAgentRuntimeIDList struct {
	mu  sync.Mutex
	ids []string
}

// NewActiveAgentRuntimeIDList creates a new instance.
func NewActiveAgentRuntimeIDList() *ActiveAgentRuntimeIDList {
	return &ActiveAgentRuntimeIDList{
		ids: make([]string, 0),
	}
}

// Add appends a runtimeID to the end of the list.
func (l *ActiveAgentRuntimeIDList) Add(runtimeID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, runtimeID)
	slog.Debug("Added Agent Runtime ID", slog.String("runtime_id", runtimeID))
}

// Pop removes the first element only if it matches the given runtimeID.
// Returns an error if the list is empty or if the ID does not match.
func (l *ActiveAgentRuntimeIDList) Pop(runtimeID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.ids) == 0 {
		slog.Error("Agent Runtime IDs is empty", slog.String("runtime_id", runtimeID))
		return errors.New("cannot pop: list is empty")
	}

	if l.ids[0] != runtimeID {
		slog.Debug("runtimeID does not match the head of the list", slog.String("runtime_id", runtimeID),
			slog.Any("list", l.ids))

		return errors.New("runtimeID does not match the head of the list")
	}

	l.ids = l.ids[1:]
	slog.Debug("Removed Agent Runtime ID", slog.String("runtime_id", runtimeID))
	return nil
}

// IsRuntimeIDActive checks if the given ID is currently active (at the head).
func (l *ActiveAgentRuntimeIDList) IsRuntimeIDActive(runtimeID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.ids) == 0 {
		return false
	}

	return l.ids[0] == runtimeID
}

// Clear   emptys the list
func (l *ActiveAgentRuntimeIDList) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = l.ids[:0] // Efficient way to clear a slice
}
