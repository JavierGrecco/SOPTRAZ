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
		{"artifact", "artifact-descriptor.example.json"},
		{"process-profile", "process-profile.example.json"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			if err := ValidateDocument(tc.kind, readExample(t, tc.name)); err != nil {
				t.Fatalf("example should validate: %v", err)
			}
		})
	}
}

func TestArtifactDescriptorRejectsMalformedSemanticVersion(t *testing.T) {
	raw := string(readExample(t, "artifact-descriptor.example.json"))
	invalid := strings.Replace(raw, `"version": "5.24.1"`, `"version": "5.24."`, 1)
	if err := ValidateDocument("artifact", []byte(invalid)); err == nil {
		t.Fatal("artifact descriptor must reject malformed SemVer")
	}
}
func TestArtifactDescriptorRejectsInvalidSizeAndPlatform(t *testing.T) {
	raw := string(readExample(t, "artifact-descriptor.example.json"))
	invalid := strings.Replace(raw, `"sizeBytes": 1048576`, `"sizeBytes": 0`, 1)
	if err := ValidateDocument("artifact", []byte(invalid)); err == nil {
		t.Fatal("artifact descriptor must reject sizeBytes = 0")
	}
	invalid = strings.Replace(raw, `"os": "linux"`, `"os": "darwin"`, 1)
	invalid = strings.Replace(invalid, `"architecture": "amd64"`, `"architecture": "386"`, 1)
	if err := ValidateDocument("artifact", []byte(invalid)); err == nil {
		t.Fatal("artifact descriptor must reject unsupported OS/architecture combinations")
	}
}
func TestCheckpointSchemaAcceptsEd25519AndECDSAP256(t *testing.T) {
	raw := string(readExample(t, "audit-checkpoint.example.json"))
	for _, algorithm := range []string{"ed25519", "ecdsa-p256-sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			candidate := strings.Replace(raw, `"signatureAlgorithm": "ed25519"`, `"signatureAlgorithm": "`+algorithm+`"`, 1)
			if candidate == raw && algorithm != "ed25519" {
				t.Fatal("test fixture did not replace signatureAlgorithm")
			}
			if err := ValidateDocument("checkpoint", []byte(candidate)); err != nil {
				t.Fatalf("checkpoint schema rejected %s: %v", algorithm, err)
			}
		})
	}
}

func TestCheckpointSchemaRejectsUnsupportedSignatureAlgorithm(t *testing.T) {
	raw := string(readExample(t, "audit-checkpoint.example.json"))
	unsupported := strings.Replace(raw, `"signatureAlgorithm": "ed25519"`, `"signatureAlgorithm": "rsa-pss"`, 1)
	if unsupported == raw {
		t.Fatal("test fixture did not replace signatureAlgorithm")
	}
	if err := ValidateDocument("checkpoint", []byte(unsupported)); err == nil {
		t.Fatal("unsupported checkpoint signature algorithm must be rejected")
	}
}

func TestStableManifestAcceptsSupportedSignatureAlgorithms(t *testing.T) {
	base := string(readExample(t, "module-manifest.example.json"))
	base = strings.Replace(base, `"supportStatus": "experimental"`, `"supportStatus": "stable"`, 1)
	base = strings.Replace(base, `"integrity": {`, `"integrity": {
    "signature": {
      "algorithm": "ed25519",
      "keyId": "`+strings.Repeat("b", 64)+`",
      "value": "`+strings.Repeat("A", 86)+`=="
    },`, 1)
	for _, algorithm := range []string{"ed25519", "ecdsa-p256-sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			candidate := strings.Replace(base, `"algorithm": "ed25519"`, `"algorithm": "`+algorithm+`"`, 1)
			if err := ValidateDocument("manifest", []byte(candidate)); err != nil {
				t.Fatalf("stable manifest should accept %s: %v", algorithm, err)
			}
		})
	}
}

func TestStableManifestRejectsUnsupportedSignatureAlgorithm(t *testing.T) {
	base := string(readExample(t, "module-manifest.example.json"))
	base = strings.Replace(base, `"supportStatus": "experimental"`, `"supportStatus": "stable"`, 1)
	base = strings.Replace(base, `"integrity": {`, `"integrity": {
    "signature": {
      "algorithm": "ed25519",
      "keyId": "`+strings.Repeat("b", 64)+`",
      "value": "`+strings.Repeat("A", 86)+`=="
    },`, 1)
	candidate := strings.Replace(base, `"algorithm": "ed25519"`, `"algorithm": "rsa-pss"`, 1)
	if err := ValidateDocument("manifest", []byte(candidate)); err == nil {
		t.Fatal("stable manifest must reject unsupported signature algorithms")
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

func TestProcessProfileRejectsShellAndUnknownExecutionFields(t *testing.T) {
	raw := string(readExample(t, "process-profile.example.json"))
	for _, candidate := range []string{
		strings.Replace(raw, `"executionMode": "direct_argv"`, `"executionMode": "shell"`, 1),
		strings.Replace(raw, `"networkAccess": "none"`, `"networkAccess": "read"`, 1),
		strings.Replace(raw, `"artifactId": "soptraz.osquery"`, `"artifactId": "../osquery"`, 1),
		strings.Replace(raw, `"fixedArguments": [`, `"command": "sh -c", "fixedArguments": [`, 1),
	} {
		if err := ValidateDocument("process-profile", []byte(candidate)); err == nil {
			t.Errorf("unsafe process profile was accepted: %s", candidate)
		}
	}
}

func TestProcessProfileRejectsControlCharactersAndExitCodeWithoutSuccess(t *testing.T) {
	raw := string(readExample(t, "process-profile.example.json"))
	control := strings.Replace(raw, `"SELECT name, version, build FROM os_version;"`, "SELECT name,\\n version FROM os_version;", 1)
	if err := ValidateDocument("process-profile", []byte(control)); err == nil {
		t.Fatal("process profile must reject control characters in fixed arguments")
	}
	noSuccess := strings.Replace(raw, `"acceptedExitCodes": [0]`, `"acceptedExitCodes": [1]`, 1)
	if err := ValidateDocument("process-profile", []byte(noSuccess)); err == nil {
		t.Fatal("process profile must include exit code zero")
	}
}
