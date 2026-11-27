package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRejectsDuplicateNamesAtEveryDepth(t *testing.T) {
	cases := []string{
		`{"key":"first","key":"second"}`,
		`{"metadata":{"owner":"first","owner":"second"}}`,
		`{"evidence":[{"path":"first","path":"second"}]}`,
		`{"role":"user","\u0072ole":"admin"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, err := Decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "clave JSON duplicada") {
				t.Fatalf("Decode() error = %v, want duplicate-key rejection", err)
			}
		})
	}
}

func TestDecodeAllowsSameNameInDifferentObjects(t *testing.T) {
	value, err := Decode([]byte(`{"left":{"id":1},"right":{"id":2}}`))
	if err != nil {
		t.Fatalf("Decode() rejected independent object scopes: %v", err)
	}
	if _, ok := value.(map[string]any); !ok {
		t.Fatalf("Decode() returned %T, want object", value)
	}
}

func TestDecodeRejectsInvalidUTF8(t *testing.T) {
	raw := append([]byte(`{"value":"`), 0xff)
	raw = append(raw, []byte(`"}`)...)
	if _, err := Decode(raw); err == nil || !strings.Contains(strings.ToLower(err.Error()), "utf-8") {
		t.Fatalf("Decode() error = %v, want invalid UTF-8 rejection", err)
	}
}

func TestDecodeRejectsTrailingValuesAndMalformedJSON(t *testing.T) {
	for _, raw := range []string{`{"ok":true} {}`, `{"ok":`, `[1,2`} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("Decode(%q) should fail", raw)
		}
	}
}

func TestDecodePreservesLargeIntegerPrecision(t *testing.T) {
	value, err := Decode([]byte(`{"sequence":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	number, ok := object["sequence"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("sequence = %#v, want exact json.Number", object["sequence"])
	}
}

func TestDecodeRejectsExcessiveNesting(t *testing.T) {
	raw := []byte(strings.Repeat("[", MaxNestingDepth+2) + "0" + strings.Repeat("]", MaxNestingDepth+2))
	if _, err := Decode(raw); err == nil || !strings.Contains(err.Error(), "profundidad") {
		t.Fatalf("Decode() error = %v, want nesting-depth rejection", err)
	}
}

func TestDecodeRejectsUnpairedUTF16SurrogateEscapes(t *testing.T) {
	cases := []string{
		`{"value":"\uD800"}`,
		`{"value":"\uDEAD"}`,
		`{"\uDC00":"value"}`,
		`{"value":"\uD800\u0041"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, err := Decode([]byte(raw)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "sustituto") {
				t.Fatalf("Decode() error = %v, want unpaired-surrogate rejection", err)
			}
		})
	}
}

func TestDecodeAcceptsValidUTF16PairAndEscapedBackslash(t *testing.T) {
	validPair := []byte(`{"value":"\uD83D\uDE00"}`)
	value, err := Decode(validPair)
	if err != nil {
		t.Fatalf("Decode() rejected valid surrogate pair: %v", err)
	}
	if got := value.(map[string]any)["value"]; got != "😀" {
		t.Fatalf("decoded value = %#v, want emoji", got)
	}

	literalEscape := []byte(`{"value":"\\uD800"}`)
	value, err = Decode(literalEscape)
	if err != nil {
		t.Fatalf("Decode() rejected escaped backslash text: %v", err)
	}
	if got := value.(map[string]any)["value"]; got != `\uD800` {
		t.Fatalf("decoded literal = %#v, want literal escape text", got)
	}
}
