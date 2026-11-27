// Package strictjson decodes JSON while rejecting ambiguous object names,
// malformed UTF-8, and unpaired UTF-16 escapes before application validation.
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
	if err := validateEscapedSurrogates(raw); err != nil {
		return nil, err
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

// validateEscapedSurrogates rejects unpaired UTF-16 surrogate escapes before
// encoding/json can replace them with U+FFFD. A valid high/low pair is kept.
func validateEscapedSurrogates(raw []byte) error {
	inString := false
	for index := 0; index < len(raw); {
		switch raw[index] {
		case '"':
			inString = !inString
			index++
		case '\\':
			if !inString || index+1 >= len(raw) {
				index++
				continue
			}
			if raw[index+1] != 'u' {
				index += 2
				continue
			}
			if index+6 > len(raw) {
				return nil // The JSON decoder reports the incomplete escape.
			}
			unit, valid := parseHexUnit(raw[index+2 : index+6])
			if !valid {
				index += 6 // Let the JSON decoder report malformed hexadecimal.
				continue
			}
			switch {
			case unit >= 0xD800 && unit <= 0xDBFF:
				if index+12 > len(raw) || raw[index+6] != '\\' || raw[index+7] != 'u' {
					return fmt.Errorf("escape UTF-16 alto sin un par sustituto bajo")
				}
				low, valid := parseHexUnit(raw[index+8 : index+12])
				if !valid {
					index += 6
					continue
				}
				if low < 0xDC00 || low > 0xDFFF {
					return fmt.Errorf("escape UTF-16 alto seguido por una unidad que no es sustituto bajo")
				}
				index += 12
			case unit >= 0xDC00 && unit <= 0xDFFF:
				return fmt.Errorf("escape UTF-16 bajo sin un par sustituto alto")
			default:
				index += 6
			}
		default:
			index++
		}
	}
	return nil
}

func parseHexUnit(value []byte) (uint16, bool) {
	if len(value) != 4 {
		return 0, false
	}
	var unit uint16
	for _, digit := range value {
		unit <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			unit |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			unit |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			unit |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return unit, true
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
