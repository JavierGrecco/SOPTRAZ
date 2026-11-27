package audit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

const (
	SchemaVersion       = "1.0.0"
	HashAlgorithm       = "sha256"
	MaxPayloadBytes     = 256 << 10
	hashDomainSeparator = "SOPTRAZ-AUDIT-EVENT-V1\x00"
)

// Actor identifies the human or system component responsible for an event.
type Actor struct {
	Type    string `json:"type"`
	ActorID string `json:"actorId"`
}

// Event is an immutable, ordered record in one intervention's audit chain.
type Event struct {
	SchemaVersion  string          `json:"schemaVersion"`
	EventID        string          `json:"eventId"`
	InterventionID string          `json:"interventionId"`
	Sequence       int64           `json:"sequence"`
	EventType      string          `json:"eventType"`
	OccurredAt     string          `json:"occurredAt"`
	Actor          Actor           `json:"actor"`
	Payload        json.RawMessage `json:"payload"`
	PreviousHash   string          `json:"previousHash,omitempty"`
	Hash           string          `json:"hash"`
}

// Draft is the caller-supplied portion of an event. Sequence, previous hash,
// and event hash are assigned by the store inside the append transaction.
type Draft struct {
	EventID        string
	InterventionID string
	EventType      string
	OccurredAt     time.Time
	Actor          Actor
	Payload        json.RawMessage
}

// Store is the persistence boundary shared by portable and server editions.
type Store interface {
	Append(ctx context.Context, draft Draft) (Event, error)
	List(ctx context.Context, interventionID string) ([]Event, error)
	Verify(ctx context.Context, interventionID string) error
	Close() error
}

// NormalizePayload parses an object payload and returns deterministic JSON.
// JSON object keys are emitted in sorted order by encoding/json; numbers are
// retained as json.Number to avoid float conversion during canonicalization.
func NormalizePayload(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("el payload de auditoría está vacío")
	}
	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("el payload supera el límite de %d bytes", MaxPayloadBytes)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("el payload contiene UTF-8 inválido")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("payload JSON inválido: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("el payload contiene más de un documento JSON")
		}
		return nil, fmt.Errorf("contenido adicional en el payload: %w", err)
	}

	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf("el payload de auditoría debe ser un objeto JSON")
	}

	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("no se pudo canonicalizar el payload: %w", err)
	}
	return json.RawMessage(canonical), nil
}

// decodeJSONValue builds a JSON value while rejecting duplicate object keys.
// encoding/json otherwise accepts duplicate keys and keeps the last value,
// which can make two downstream parsers interpret audit evidence differently.
func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}

	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("se esperaba una clave de objeto JSON")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("clave JSON duplicada %q", key)
			}

			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}

		closing, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if closing != json.Delim('}') {
			return nil, fmt.Errorf("objeto JSON sin cierre")
		}
		return object, nil

	case '[':
		values := make([]any, 0)
		for decoder.More() {
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}

		closing, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if closing != json.Delim(']') {
			return nil, fmt.Errorf("array JSON sin cierre")
		}
		return values, nil

	default:
		return nil, fmt.Errorf("delimitador JSON inesperado %q", delimiter)
	}
}

// ComputeHash computes the event hash from its immutable fields, including the
// previous event hash. Hash is excluded from its own preimage.
func ComputeHash(event Event) (string, error) {
	payload, err := NormalizePayload(event.Payload)
	if err != nil {
		return "", err
	}

	material := struct {
		SchemaVersion  string          `json:"schemaVersion"`
		EventID        string          `json:"eventId"`
		InterventionID string          `json:"interventionId"`
		Sequence       int64           `json:"sequence"`
		EventType      string          `json:"eventType"`
		OccurredAt     string          `json:"occurredAt"`
		Actor          Actor           `json:"actor"`
		Payload        json.RawMessage `json:"payload"`
		PreviousHash   string          `json:"previousHash,omitempty"`
	}{
		SchemaVersion:  event.SchemaVersion,
		EventID:        event.EventID,
		InterventionID: event.InterventionID,
		Sequence:       event.Sequence,
		EventType:      event.EventType,
		OccurredAt:     event.OccurredAt,
		Actor:          event.Actor,
		Payload:        payload,
		PreviousHash:   event.PreviousHash,
	}

	encoded, err := json.Marshal(material)
	if err != nil {
		return "", fmt.Errorf("no se pudo serializar el evento para calcular su hash: %w", err)
	}

	hasher := sha256.New()
	_, _ = hasher.Write([]byte(hashDomainSeparator))
	_, _ = hasher.Write(encoded)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// NewEventID generates a UUID v4 using the operating system's cryptographic RNG.
func NewEventID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("no se pudo generar el identificador del evento: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded), nil
}
