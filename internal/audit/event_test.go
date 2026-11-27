package audit

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNormalizePayloadCanonicalizesObjectKeys(t *testing.T) {
	first, err := NormalizePayload([]byte(`{"z":1,"a":{"y":2,"b":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizePayload([]byte(`{"a":{"b":3,"y":2},"z":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical payloads differ: %s != %s", first, second)
	}
}

func TestNormalizePayloadRejectsInvalidOrNonObjectJSON(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte("[]"),
		[]byte("null"),
		[]byte(`{"ok":true} {}`),
		[]byte("{"),
	}
	for _, raw := range cases {
		if _, err := NormalizePayload(raw); err == nil {
			t.Errorf("NormalizePayload(%q) should fail", raw)
		}
	}
}

func TestComputeHashIsDeterministicAndChainSensitive(t *testing.T) {
	event := Event{
		SchemaVersion:  SchemaVersion,
		EventID:        "71d8e54a-1504-4eeb-8c13-84cc5083e541",
		InterventionID: "2aef4e6d-9d50-4cb6-9c96-22a26abf1587",
		Sequence:       1,
		EventType:      "intervention.created",
		OccurredAt:     "2026-11-27T19:00:00Z",
		Actor:          Actor{Type: "user", ActorID: "technician-001"},
		Payload:        json.RawMessage(`{"category":"network","source":"field"}`),
	}

	first, err := ComputeHash(event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeHash(event)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash is not deterministic: %s != %s", first, second)
	}

	event.PreviousHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	chained, err := ComputeHash(event)
	if err != nil {
		t.Fatal(err)
	}
	if chained == first {
		t.Fatal("previous hash must be part of the hash preimage")
	}
}

func TestNewEventIDReturnsUUIDShape(t *testing.T) {
	id, err := NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("unexpected UUID shape: %q", id)
	}
}
