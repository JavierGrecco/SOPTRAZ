package contracts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "examples", "contracts", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example %s: %v", name, err)
	}
	return raw
}

func TestExampleContractsValidate(t *testing.T) {
	cases := []struct {
		kind string
		name string
	}{
		{"manifest", "module-manifest.example.json"},
		{"action", "action-definition.example.json"},
		{"intervention", "intervention.example.json"},
		{"event", "intervention-event.example.json"},
		{"checkpoint", "audit-checkpoint.example.json"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			if err := ValidateDocument(tc.kind, readExample(t, tc.name)); err != nil {
				t.Fatalf("example should validate: %v", err)
			}
		})
	}
}

func TestManifestRejectsUnknownFieldsAndUnsignedStableModules(t *testing.T) {
	raw := string(readExample(t, "module-manifest.example.json"))
	unknown := strings.Replace(raw, "\"description\":", "\"unexpected\": true, \"description\":", 1)
	if err := ValidateDocument("manifest", []byte(unknown)); err == nil {
		t.Fatal("manifest with unknown property should be rejected")
	}

	unsignedStable := strings.Replace(raw, "\"supportStatus\": \"experimental\"", "\"supportStatus\": \"stable\"", 1)
	if err := ValidateDocument("manifest", []byte(unsignedStable)); err == nil {
		t.Fatal("stable module without signature should be rejected")
	}
}

func TestActionRequiresApprovalForHighRiskOperations(t *testing.T) {
	raw := string(readExample(t, "action-definition.example.json"))
	highRisk := strings.Replace(raw, "\"risk\": \"read_only\"", "\"risk\": \"high\"", 1)
	if err := ValidateDocument("action", []byte(highRisk)); err == nil {
		t.Fatal("high-risk action without approval must be rejected")
	}

	destructive := strings.Replace(raw, "\"effects\": [\"host.read\"]", "\"effects\": [\"data.delete\"]", 1)
	if err := ValidateDocument("action", []byte(destructive)); err == nil {
		t.Fatal("destructive effect must require critical risk, admin and approval")
	}
}

func TestActionRequiresAdminAndApprovalForCriticalRisk(t *testing.T) {
	raw := string(readExample(t, "action-definition.example.json"))
	unsafe := strings.Replace(raw, "\"risk\": \"read_only\"", "\"risk\": \"critical\"", 1)
	if err := ValidateDocument("action", []byte(unsafe)); err == nil {
		t.Fatal("critical action without admin and approval must be rejected")
	}
}

func TestActionCompilesNestedSchemasAndBlocksRemoteReferences(t *testing.T) {
	raw := string(readExample(t, "action-definition.example.json"))

	invalidNested := strings.Replace(raw,
		"\"properties\": {},",
		"\"properties\": {\"device\": {\"type\": \"not-a-json-schema-type\"}},",
		1,
	)
	if err := ValidateDocument("action", []byte(invalidNested)); err == nil {
		t.Fatal("invalid nested parameter schema must be rejected")
	}

	remoteReference := strings.Replace(raw,
		"\"inputSchema\": {",
		"\"inputSchema\": {\"$ref\": \"https://example.invalid/external.json\",",
		1,
	)
	if err := ValidateDocument("action", []byte(remoteReference)); err == nil {
		t.Fatal("external parameter-schema references must be rejected")
	}
}

func TestUnknownKindMalformedJSONTrailingJSONAndSizeLimitAreRejected(t *testing.T) {
	if err := ValidateDocument("unknown", []byte("{}")); err == nil {
		t.Fatal("unknown contract kind must be rejected")
	}
	if err := ValidateDocument("manifest", []byte("{")); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}
	if err := ValidateDocument("manifest", []byte("{} {}")); err == nil {
		t.Fatal("multiple JSON documents must be rejected")
	}
	if err := ValidateDocument("manifest", make([]byte, MaxDocumentBytes+1)); err == nil {
		t.Fatal("oversized document must be rejected")
	}
}

func TestValidateDocumentRejectsDuplicateKeysIncludingEscapedNames(t *testing.T) {
	raw := []byte(`{"schemaVersion":"1.0.0","metadata":{"owner":"one","o\u0077ner":"two"}}`)
	err := ValidateDocument("manifest", raw)
	if err == nil || !strings.Contains(err.Error(), "clave JSON duplicada") {
		t.Fatalf("ValidateDocument() error = %v, want duplicate-key rejection", err)
	}
}

func TestValidateDocumentRejectsInvalidUTF8(t *testing.T) {
	raw := append([]byte(`{"schemaVersion":"1.0.0","description":"`), 0xff)
	raw = append(raw, []byte(`"}`)...)
	err := ValidateDocument("manifest", raw)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "utf-8") {
		t.Fatalf("ValidateDocument() error = %v, want invalid UTF-8 rejection", err)
	}
}
