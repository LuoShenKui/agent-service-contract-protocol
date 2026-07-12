package server

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

// IdempotencyResult identifies whether a request should run, replay, or fail.
type IdempotencyResult int

const (
	IdempotencyNew IdempotencyResult = iota
	IdempotencyReplay
	IdempotencyConflict
	IdempotencyInProgress
)

// StoredResponse is the exact replayable HTTP result of a mutating operation.
type StoredResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	ExpiresAt  time.Time
}

type idempotencyEntry struct {
	RequestDigest string
	InProgress    bool
	Response      StoredResponse
}

// IdempotencyBackend atomically claims mutation keys and stores exact HTTP
// responses. A distributed implementation must make Begin and Complete durable
// across all service replicas.
type IdempotencyBackend interface {
	Begin(scope, key, requestDigest string, retention time.Duration) (IdempotencyResult, StoredResponse, error)
	Complete(scope, key string, response StoredResponse) error
	Release(scope, key, requestDigest string) error
}

// IdempotencyStore provides atomic duplicate detection for a single server
// process. A distributed deployment must use a shared transactional store.
type IdempotencyStore struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
	now     func() time.Time
}

// NewIdempotencyStore creates an empty store.
func NewIdempotencyStore() *IdempotencyStore {
	return &IdempotencyStore{
		entries: make(map[string]idempotencyEntry),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Begin atomically claims a key. Scope must include the authenticated principal
// and operation so two tenants may safely use the same random key.
func (s *IdempotencyStore) Begin(scope, key, requestDigest string, retention time.Duration) (IdempotencyResult, StoredResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	storageKey := scope + "\x00" + key
	if entry, exists := s.entries[storageKey]; exists {
		// An in-progress or unknown outcome is never released merely because a
		// replay-retention timestamp elapsed. Doing so could let a retry repeat a
		// real-world side effect. Reconciliation or an explicit proven-no-effect
		// Release call must resolve the claim.
		if entry.RequestDigest != requestDigest {
			return IdempotencyConflict, StoredResponse{}, nil
		}
		if entry.InProgress {
			return IdempotencyInProgress, StoredResponse{}, nil
		}
		if !entry.Response.ExpiresAt.IsZero() && s.now().After(entry.Response.ExpiresAt) {
			delete(s.entries, storageKey)
		} else {
			return IdempotencyReplay, cloneStoredResponse(entry.Response), nil
		}
	}

	s.entries[storageKey] = idempotencyEntry{
		RequestDigest: requestDigest,
		InProgress:    true,
		Response: StoredResponse{
			ExpiresAt: s.now().Add(retention),
		},
	}
	return IdempotencyNew, StoredResponse{}, nil
}

// Complete stores the exact response for future retries.
func (s *IdempotencyStore) Complete(scope, key string, response StoredResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storageKey := scope + "\x00" + key
	entry, exists := s.entries[storageKey]
	if !exists {
		return errors.New("idempotency claim not found")
	}
	response.Headers = response.Headers.Clone()
	response.Body = append([]byte(nil), response.Body...)
	if response.ExpiresAt.IsZero() {
		response.ExpiresAt = entry.Response.ExpiresAt
	}
	entry.InProgress = false
	entry.Response = response
	s.entries[storageKey] = entry
	return nil
}

// Release removes an in-progress claim only when the caller proves that no
// contracted or billing side effect crossed the execution boundary. It also
// requires the original request digest, preventing an unrelated request from
// reopening another operation's key. Unknown or post-effect outcomes must never
// call Release; they remain in progress until reconciliation completes them.
func (s *IdempotencyStore) Release(scope, key, requestDigest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storageKey := scope + "\x00" + key
	entry, exists := s.entries[storageKey]
	if !exists {
		return errors.New("idempotency claim not found")
	}
	if !entry.InProgress {
		return errors.New("completed idempotency claim cannot be released")
	}
	if entry.RequestDigest != requestDigest {
		return errors.New("idempotency release digest mismatch")
	}
	delete(s.entries, storageKey)
	return nil
}

func cloneStoredResponse(response StoredResponse) StoredResponse {
	response.Headers = response.Headers.Clone()
	response.Body = append([]byte(nil), response.Body...)
	return response
}
