package utils

import "sync"

type StringSet struct {
	mu    sync.RWMutex
	items map[string]struct{}
}

// NewStringSet initializes a new thread-safe string set
func NewStringSet() *StringSet {
	return &StringSet{
		items: make(map[string]struct{}),
	}
}

// Add inserts a string into the set
func (s *StringSet) Add(item string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[item] = struct{}{}
}

// Exists checks if a string is in the set
func (s *StringSet) Exists(item string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.items[item]
	return ok
}

// Clear removes all items from the set
func (s *StringSet) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]struct{})
}

// Set replaces the set contents with the given slice of strings
func (s *StringSet) Set(items []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newItems := make(map[string]struct{}, len(items))
	for _, item := range items {
		newItems[item] = struct{}{}
	}
	s.items = newItems
}

// IsEmpty returns true if the set has no items
func (s *StringSet) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) == 0
}

// Remove  take an item from the lists
func (s *StringSet) Remove(item string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, item)
}
