package server

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// Store persists immutable offers and quotes plus versioned durable tasks.
// Implementations shared by multiple replicas must provide transactional
// consistency and defensive isolation between tenants.
type Store interface {
	PutOffer(offer StoredOffer) error
	GetOffer(id string) (StoredOffer, bool, error)
	PutQuote(quote ascp.Quote) error
	GetQuote(id string) (ascp.Quote, bool, error)
	PutTask(task ascp.Task) error
	GetTask(id string) (ascp.Task, bool, error)
	UpdateTask(id string, update func(*ascp.Task) error) (ascp.Task, error)
	PutInvocation(record InvocationRecord) error
	GetInvocation(id string) (InvocationRecord, bool, error)
}

// InvocationRecord is the durable ownership and response record for one direct
// invocation. A production store should persist this alongside the audit outbox
// before returning the signed response.
type InvocationRecord struct {
	InvocationID  string
	Actor         ascp.EntityRef
	Principal     ascp.EntityRef
	RequestDigest string
	Response      ascp.DirectInvocationResponse
}

// MemoryStore is a concurrency-safe reference store. Production services should
// replace it with a transactional database and an outbox for external effects.
type MemoryStore struct {
	mu          sync.RWMutex
	offers      map[string]StoredOffer
	quotes      map[string]ascp.Quote
	tasks       map[string]ascp.Task
	invocations map[string]InvocationRecord
}

// NewMemoryStore creates an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		offers:      make(map[string]StoredOffer),
		quotes:      make(map[string]ascp.Quote),
		tasks:       make(map[string]ascp.Task),
		invocations: make(map[string]InvocationRecord),
	}
}

// PutInvocation stores a direct invocation and rejects replacement. The
// idempotency subsystem handles exact HTTP replay; this record supports later
// result and audit retrieval.
func (s *MemoryStore) PutInvocation(record InvocationRecord) error {
	copy, err := cloneJSON(record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.invocations[record.InvocationID]; exists {
		return errors.New("invocation already exists")
	}
	s.invocations[record.InvocationID] = copy
	return nil
}

// GetInvocation returns a defensive copy.
func (s *MemoryStore) GetInvocation(id string) (InvocationRecord, bool, error) {
	s.mu.RLock()
	record, ok := s.invocations[id]
	s.mu.RUnlock()
	if !ok {
		return InvocationRecord{}, false, nil
	}
	copy, err := cloneJSON(record)
	if err != nil {
		return InvocationRecord{}, false, err
	}
	return copy, true, nil
}

// PutOffer stores an immutable capability offer.
func (s *MemoryStore) PutOffer(offer StoredOffer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	offer.RequiredScopes = append([]string(nil), offer.RequiredScopes...)
	if offer.Budget != nil {
		budget := *offer.Budget
		offer.Budget = &budget
	}
	s.offers[offer.OfferID] = offer
	return nil
}

// GetOffer returns a defensive copy.
func (s *MemoryStore) GetOffer(id string) (StoredOffer, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	offer, ok := s.offers[id]
	offer.RequiredScopes = append([]string(nil), offer.RequiredScopes...)
	if offer.Budget != nil {
		budget := *offer.Budget
		offer.Budget = &budget
	}
	return offer, ok, nil
}

// PutQuote stores a signed quote.
func (s *MemoryStore) PutQuote(quote ascp.Quote) error {
	copy, err := cloneJSON(quote)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotes[quote.QuoteID] = copy
	return nil
}

// GetQuote returns a defensive copy.
func (s *MemoryStore) GetQuote(id string) (ascp.Quote, bool, error) {
	s.mu.RLock()
	quote, ok := s.quotes[id]
	s.mu.RUnlock()
	if !ok {
		return ascp.Quote{}, false, nil
	}
	copy, err := cloneJSON(quote)
	if err != nil {
		return ascp.Quote{}, false, err
	}
	return copy, true, nil
}

// PutTask creates a task and rejects accidental replacement.
func (s *MemoryStore) PutTask(task ascp.Task) error {
	copy, err := cloneJSON(task)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[task.TaskID]; exists {
		return errors.New("task already exists")
	}
	s.tasks[task.TaskID] = copy
	return nil
}

// GetTask returns a defensive copy.
func (s *MemoryStore) GetTask(id string) (ascp.Task, bool, error) {
	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()
	if !ok {
		return ascp.Task{}, false, nil
	}
	copy, err := cloneJSON(task)
	if err != nil {
		return ascp.Task{}, false, err
	}
	return copy, true, nil
}

// UpdateTask applies a mutation under a lock and increments the optimistic
// concurrency version exactly once.
func (s *MemoryStore) UpdateTask(id string, update func(*ascp.Task) error) (ascp.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return ascp.Task{}, errors.New("task not found")
	}
	if err := update(&task); err != nil {
		return ascp.Task{}, err
	}
	task.Version++
	s.tasks[id] = task
	return cloneJSON(task)
}

func cloneJSON[T any](value T) (T, error) {
	var copy T
	encoded, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}
