// Package audit implements the append-only, hash-chained audit profile required
// by ASCP. The in-memory implementation is suitable for tests and demos; a
// production service should persist events in immutable or write-once storage.
package audit

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// JSONSigner is the minimal signing boundary required by the audit log. It can
// be backed by the in-process Ed25519 signer or a KMS/HSM adapter.
type JSONSigner interface {
	SignJSON(value any) (ascp.Signature, error)
}

// Log stores independent event chains by protocol resource ID.
type Log struct {
	mu     sync.RWMutex
	signer JSONSigner
	now    func() time.Time
	events map[string][]ascp.AuditEvent
}

// NewLog creates a signed audit log.
func NewLog(signer JSONSigner) *Log {
	return &Log{
		signer: signer,
		now:    func() time.Time { return time.Now().UTC() },
		events: make(map[string][]ascp.AuditEvent),
	}
}

// signedEventPayload is the exact object embedded in the event JWS. Keeping the
// signature outside the payload prevents recursive signing.
type signedEventPayload struct {
	EventID      string          `json:"event_id"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Sequence     int64           `json:"sequence"`
	Type         string          `json:"type"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Actor        ascp.EntityRef  `json:"actor"`
	Data         json.RawMessage `json:"data,omitempty"`
	DataDigest   string          `json:"data_digest"`
	PreviousHash string          `json:"previous_hash,omitempty"`
	EventHash    string          `json:"event_hash"`
}

// Append adds one signed event to a resource chain and returns a defensive copy.
func (l *Log) Append(resourceID, eventType string, actor ascp.EntityRef, data any) (ascp.AuditEvent, error) {
	if resourceID == "" {
		return ascp.AuditEvent{}, errors.New("audit resource ID is required")
	}
	if eventType == "" {
		return ascp.AuditEvent{}, errors.New("audit event type is required")
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return ascp.AuditEvent{}, fmt.Errorf("marshal audit data: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	sequence := int64(len(l.events[resourceID]) + 1)
	previousHash := ""
	if sequence > 1 {
		previousHash = l.events[resourceID][sequence-2].EventHash
	}

	eventID, err := ascp.NewID("evt")
	if err != nil {
		return ascp.AuditEvent{}, err
	}
	occurredAt := l.now()
	dataDigest := ascp.SHA256Digest(dataBytes)
	resourceType := inferResourceType(resourceID)

	// The chain hash uses a deterministic tuple rather than prose or map order.
	chainMaterial := []byte(
		eventID + "\n" + resourceType + "\n" + resourceID + "\n" + strconv.FormatInt(sequence, 10) + "\n" +
			eventType + "\n" + occurredAt.Format(time.RFC3339Nano) + "\n" +
			actor.Type + "\n" + actor.ID + "\n" + dataDigest + "\n" + previousHash,
	)
	eventHash := ascp.SHA256Digest(chainMaterial)

	payload := signedEventPayload{
		EventID:      eventID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Sequence:     sequence,
		Type:         eventType,
		OccurredAt:   occurredAt,
		Actor:        actor,
		Data:         dataBytes,
		DataDigest:   dataDigest,
		PreviousHash: previousHash,
		EventHash:    eventHash,
	}
	signature, err := l.signer.SignJSON(payload)
	if err != nil {
		return ascp.AuditEvent{}, fmt.Errorf("sign audit event: %w", err)
	}

	event := ascp.AuditEvent{
		EventID:      payload.EventID,
		ResourceType: payload.ResourceType,
		ResourceID:   payload.ResourceID,
		Sequence:     payload.Sequence,
		Type:         payload.Type,
		OccurredAt:   payload.OccurredAt,
		Actor:        payload.Actor,
		Data:         append(json.RawMessage(nil), payload.Data...),
		DataDigest:   payload.DataDigest,
		PreviousHash: payload.PreviousHash,
		EventHash:    payload.EventHash,
		Signature:    signature,
	}
	l.events[resourceID] = append(l.events[resourceID], event)
	return cloneEvent(event), nil
}

// List returns all events after the supplied sequence number. Passing zero
// returns the entire chain.
func (l *Log) List(resourceID string, afterSequence int64) ([]ascp.AuditEvent, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	stored := l.events[resourceID]
	result := make([]ascp.AuditEvent, 0, len(stored))
	for _, event := range stored {
		if event.Sequence > afterSequence {
			result = append(result, cloneEvent(event))
		}
	}
	return result, nil
}

// Root returns the current hash-chain root, or an empty string for a new chain.
func (l *Log) Root(resourceID string) (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	stored := l.events[resourceID]
	if len(stored) == 0 {
		return "", nil
	}
	return stored[len(stored)-1].EventHash, nil
}

// Verify validates sequence order, hash chaining, content digests, and JWS
// signatures. It can be used without access to the service database.
func Verify(events []ascp.AuditEvent, publicKey ed25519.PublicKey) error {
	previousHash := ""
	resourceType := ""
	resourceID := ""
	for index, event := range events {
		expectedSequence := int64(index + 1)
		if event.Sequence != expectedSequence {
			return fmt.Errorf("audit sequence mismatch: got %d want %d", event.Sequence, expectedSequence)
		}
		if index == 0 {
			resourceType = event.ResourceType
			resourceID = event.ResourceID
		}
		if event.ResourceType != resourceType || event.ResourceID != resourceID {
			return fmt.Errorf("audit resource changed at sequence %d", event.Sequence)
		}
		if event.PreviousHash != previousHash {
			return fmt.Errorf("audit previous hash mismatch at sequence %d", event.Sequence)
		}
		if ascp.SHA256Digest(event.Data) != event.DataDigest {
			return fmt.Errorf("audit data digest mismatch at sequence %d", event.Sequence)
		}

		chainMaterial := []byte(
			event.EventID + "\n" + event.ResourceType + "\n" + event.ResourceID + "\n" + strconv.FormatInt(event.Sequence, 10) + "\n" +
				event.Type + "\n" + event.OccurredAt.Format(time.RFC3339Nano) + "\n" +
				event.Actor.Type + "\n" + event.Actor.ID + "\n" + event.DataDigest + "\n" + event.PreviousHash,
		)
		if expected := ascp.SHA256Digest(chainMaterial); expected != event.EventHash {
			return fmt.Errorf("audit event hash mismatch at sequence %d", event.Sequence)
		}

		var signed signedEventPayload
		if err := ascp.VerifyJSON(event.Signature, publicKey, &signed); err != nil {
			return fmt.Errorf("verify event signature at sequence %d: %w", event.Sequence, err)
		}
		if signed.EventID != event.EventID || signed.EventHash != event.EventHash ||
			signed.DataDigest != event.DataDigest || signed.PreviousHash != event.PreviousHash ||
			signed.Sequence != event.Sequence || signed.ResourceType != event.ResourceType ||
			signed.ResourceID != event.ResourceID || signed.Type != event.Type ||
			signed.OccurredAt != event.OccurredAt || signed.Actor != event.Actor ||
			!bytes.Equal(signed.Data, event.Data) {
			return fmt.Errorf("signed event payload mismatch at sequence %d", event.Sequence)
		}

		previousHash = event.EventHash
	}
	return nil
}

func cloneEvent(event ascp.AuditEvent) ascp.AuditEvent {
	event.Data = append(json.RawMessage(nil), event.Data...)
	return event
}

// VerifyReceiptAnchor proves that a contract receipt's audit root is the exact
// chain root immediately before the receipt-issued event.
func VerifyReceiptAnchor(events []ascp.AuditEvent, receipt ascp.Receipt) error {
	return verifyReceiptAnchor(events, receipt.ReceiptID, receipt.AuditRoot, "ascp.receipt.issued")
}

// VerifyInvocationReceiptAnchor performs the same proof for direct invocation.
func VerifyInvocationReceiptAnchor(events []ascp.AuditEvent, receipt ascp.InvocationReceipt) error {
	return verifyReceiptAnchor(events, receipt.ReceiptID, receipt.AuditRoot, "ascp.invocation.receipt.issued")
}

func verifyReceiptAnchor(events []ascp.AuditEvent, receiptID, auditRoot, eventType string) error {
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		var data struct {
			ReceiptID string `json:"receipt_id"`
			AuditRoot string `json:"audit_root"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("decode receipt-issued audit data: %w", err)
		}
		if data.ReceiptID != receiptID {
			continue
		}
		if data.AuditRoot != auditRoot || event.PreviousHash != auditRoot {
			return errors.New("receipt audit root does not match the pre-receipt chain root")
		}
		return nil
	}
	return errors.New("receipt-issued audit event not found")
}

func inferResourceType(resourceID string) string {
	switch {
	case strings.HasPrefix(resourceID, "tsk_"):
		return "task"
	case strings.HasPrefix(resourceID, "inv_"):
		return "invocation"
	case strings.HasPrefix(resourceID, "fil_"):
		return "file"
	case strings.HasPrefix(resourceID, "quo_"):
		return "quote"
	case strings.HasPrefix(resourceID, "off_"):
		return "offer"
	default:
		return "resource"
	}
}
