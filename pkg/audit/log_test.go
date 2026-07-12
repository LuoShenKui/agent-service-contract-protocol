package audit

import (
	"encoding/json"
	"testing"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

func TestAuditChainDetectsTampering(t *testing.T) {
	t.Parallel()

	signer, err := ascp.NewSigner("audit-test")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	log := NewLog(signer)
	actor := ascp.EntityRef{Type: "agent", ID: "test"}
	if _, err := log.Append("task-1", "task.accepted", actor, map[string]any{"n": 1}); err != nil {
		t.Fatalf("Append first event: %v", err)
	}
	if _, err := log.Append("task-1", "task.succeeded", actor, map[string]any{"n": 2}); err != nil {
		t.Fatalf("Append second event: %v", err)
	}

	events, err := log.List("task-1", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := Verify(events, signer.PublicKey()); err != nil {
		t.Fatalf("Verify valid chain: %v", err)
	}

	events[0].Data = json.RawMessage(`{"n":99}`)
	if err := Verify(events, signer.PublicKey()); err == nil {
		t.Fatal("expected modified event data to be detected")
	}
}
