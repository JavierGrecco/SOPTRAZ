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

func TestNormalizePayloadRejectsDuplicateKeysAtAnyDepth(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"status":"valid","status":"tampered"}`),
		[]byte(`{"metadata":{"owner":"first","owner":"second"}}`),
		[]byte(`{"evidence":[{"path":"first","path":"second"}]}`),
		[]byte(`{"role":"user","\u0072ole":"admin"}`),
	}
	for _, raw := range cases {
		if _, err := NormalizePayload(raw); err == nil {
			t.Errorf("NormalizePayload(%q) should reject duplicate keys", raw)
		}
	}
}

func TestNormalizePayloadAllowsSameKeyInDifferentObjects(t *testing.T) {
	raw := []byte(`{"left":{"id":1},"right":{"id":2}}`)
	if _, err := NormalizePayload(raw); err != nil {
		t.Fatalf("NormalizePayload() rejected distinct object scopes: %v", err)
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

func TestNormalizePayloadRejectsInvalidUTF8(t *testing.T) {
	raw := []byte{'{', '"', 'v', 'a', 'l', 'u', 'e', '"', ':', '"', 0xff, '"', '}'}
	if _, err := NormalizePayload(raw); err == nil {
		t.Fatal("NormalizePayload() should reject invalid UTF-8")
	}
}

func TestNormalizePayloadPreservesLargeIntegerPrecision(t *testing.T) {
	raw := []byte(`{"sequence":9007199254740993}`)
	got, err := NormalizePayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"sequence":9007199254740993}`; string(got) != want {
		t.Fatalf("NormalizePayload() = %s, want %s", got, want)
	}
}

func FuzzNormalizePayloadIsIdempotent(f *testing.F) {
	f.Add([]byte(`{"actor":"technician","sequence":1}`))
	f.Add([]byte(`{"actor":"first","actor":"second"}`))
	f.Add([]byte(`{"nested":[{"key":true}]}`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		canonical, err := NormalizePayload(raw)
		if err != nil {
			return
		}
		again, err := NormalizePayload(canonical)
		if err != nil {
			t.Fatalf("canonical payload was rejected on second pass: %v", err)
		}
		if !bytes.Equal(canonical, again) {
			t.Fatalf("normalization is not idempotent: %s != %s", canonical, again)
		}
	})
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
