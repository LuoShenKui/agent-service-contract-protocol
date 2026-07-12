package server

import (
	"testing"
	"time"
)

func TestValidIdempotencyKey(t *testing.T) {
	t.Parallel()

	valid := []string{
		"0123456789abcdef",
		"task:8db74a8e-8852-4eb9-8e7d-669898f725f2",
		"opaque_key.with~symbols+safe",
	}
	for _, key := range valid {
		if !validIdempotencyKey(key) {
			t.Errorf("validIdempotencyKey(%q) = false", key)
		}
	}

	invalid := []string{
		"too-short",
		"contains a space and is long enough",
		"contains\ttab-and-is-long-enough",
		"包含不可见的非ASCII字符",
	}
	for _, key := range invalid {
		if validIdempotencyKey(key) {
			t.Errorf("validIdempotencyKey(%q) = true", key)
		}
	}
}

func TestInProgressClaimDoesNotExpireIntoDuplicateExecution(t *testing.T) {
	t.Parallel()

	store := NewIdempotencyStore()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	state, _, err := store.Begin("actor/principal|POST|/v1/commit", "0123456789abcdef", "sha256:first", time.Minute)
	if err != nil || state != IdempotencyNew {
		t.Fatalf("initial Begin = %v, %v", state, err)
	}

	// Advance beyond ordinary replay retention. The unresolved operation must
	// remain locked until reconciliation or an explicit safe Release.
	now = now.Add(24 * time.Hour)
	state, _, err = store.Begin("actor/principal|POST|/v1/commit", "0123456789abcdef", "sha256:first", time.Minute)
	if err != nil || state != IdempotencyInProgress {
		t.Fatalf("expired in-progress Begin = %v, %v; want in-progress", state, err)
	}
}

func TestCompletedReplayMayExpireAfterRetention(t *testing.T) {
	t.Parallel()

	store := NewIdempotencyStore()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	scope := "actor/principal|POST|/v1/prepare"
	key := "fedcba9876543210"
	digest := "sha256:request"

	state, _, err := store.Begin(scope, key, digest, time.Minute)
	if err != nil || state != IdempotencyNew {
		t.Fatalf("initial Begin = %v, %v", state, err)
	}
	if err := store.Complete(scope, key, StoredResponse{StatusCode: 201, Body: []byte(`{"ok":true}`)}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	now = now.Add(2 * time.Minute)
	state, _, err = store.Begin(scope, key, digest, time.Minute)
	if err != nil || state != IdempotencyNew {
		t.Fatalf("Begin after completed retention = %v, %v; want new", state, err)
	}
}
