// Package strictjson decodes JSON while rejecting ambiguous object names and
// malformed UTF-8 before application-specific validation.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxNestingDepth = 256

// Decode parses exactly one JSON value. Object names are compared after JSON
// escape decoding, so "role" and "\\u0072ole" are treated as duplicates.
// Numbers remain json.Number to preserve integer precision.
func Decode(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("el documento JSON contiene UTF-8 inválido")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	value, err := decodeValue(decoder, 0)
	if err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("el documento contiene más de un valor JSON")
		}
		return nil, fmt.Errorf("contenido adicional después del documento JSON: %w", err)
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > MaxNestingDepth {
		return nil, fmt.Errorf("el documento supera la profundidad JSON máxima de %d", MaxNestingDepth)
	}

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
				return nil, fmt.Errorf("se esperaba un nombre de miembro JSON")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("clave JSON duplicada %q", key)
			}

			value, err := decodeValue(decoder, depth+1)
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
			value, err := decodeValue(decoder, depth+1)
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
