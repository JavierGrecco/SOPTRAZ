package deviceprofile

import (
	"reflect"
	"testing"
)

func TestClassifyDeviceFormAndSMBIOSCorroborateTablet(t *testing.T) {
	profile := Classify(Observations{
		DeviceForm:         FormFactorTablet,
		SMBIOSChassisTypes: []uint16{30},
		PCSystemType:       PCSystemTypeMobile,
		Capabilities: Capabilities{
			BatteryPresent: Yes,
			Touch:          Yes,
			Pen:            Yes,
			PowerSource:    PowerSourceBattery,
		},
	})

	if profile.FormFactor != FormFactorTablet || profile.Confidence != ConfidenceHigh {
		t.Fatalf("unexpected classification: %#v", profile)
	}
	if profile.Capabilities.Touch != Yes || profile.Capabilities.Pen != Yes ||
		profile.Capabilities.BatteryPresent != Yes || profile.Capabilities.PowerSource != PowerSourceBattery {
		t.Fatalf("capabilities were not preserved: %#v", profile.Capabilities)
	}
	if len(profile.Evidence) != 3 {
		t.Fatalf("evidence count = %d, want 3: %#v", len(profile.Evidence), profile.Evidence)
	}
}

func TestClassifySMBIOSNotebookWithMobileCorroboration(t *testing.T) {
	profile := Classify(Observations{
		SMBIOSChassisTypes: []uint16{9, 10},
		PCSystemType:       PCSystemTypeMobile,
		Capabilities: Capabilities{
			BatteryPresent:   Yes,
			PhysicalKeyboard: Yes,
		},
	})
	if profile.FormFactor != FormFactorNotebook || profile.Confidence != ConfidenceHigh {
		t.Fatalf("unexpected notebook classification: %#v", profile)
	}
}

func TestClassifyDesktopWithTouchDoesNotBecomeTablet(t *testing.T) {
	profile := Classify(Observations{
		SMBIOSChassisTypes: []uint16{6},
		Capabilities: Capabilities{
			Touch:          Yes,
			BatteryPresent: No,
		},
	})
	if profile.FormFactor != FormFactorDesktop {
		t.Fatalf("form factor = %q, want desktop", profile.FormFactor)
	}
	if profile.Capabilities.Touch != Yes {
		t.Fatal("touch capability was lost")
	}
}

func TestClassifyBatteryAndTouchDoNotInferNotebookOrTablet(t *testing.T) {
	profile := Classify(Observations{
		Capabilities: Capabilities{
			BatteryPresent:   Yes,
			Touch:            Yes,
			Pen:              Yes,
			PhysicalKeyboard: Yes,
		},
	})
	if profile.FormFactor != FormFactorUnknown || profile.Confidence != ConfidenceLow {
		t.Fatalf("capabilities incorrectly inferred a form factor: %#v", profile)
	}
}

func TestClassifyPCSystemMobileAloneRemainsUnknown(t *testing.T) {
	profile := Classify(Observations{
		PCSystemType: PCSystemTypeMobile,
		Capabilities: Capabilities{BatteryPresent: Yes},
	})
	if profile.FormFactor != FormFactorUnknown {
		t.Fatalf("PCSystemType Mobile alone was classified as %q", profile.FormFactor)
	}
	if !containsWarning(profile.Warnings, "PCSystemType Mobile no distingue por sí solo notebook, tablet o convertible.") {
		t.Fatalf("missing ambiguity warning: %#v", profile.Warnings)
	}
}

func TestClassifyPCSystemDesktopAloneIsLowConfidence(t *testing.T) {
	profile := Classify(Observations{PCSystemType: PCSystemTypeDesktop})
	if profile.FormFactor != FormFactorDesktop || profile.Confidence != ConfidenceLow {
		t.Fatalf("unexpected low-confidence desktop classification: %#v", profile)
	}
}

func TestClassifyConvertibleKeepsPostureSeparate(t *testing.T) {
	profile := Classify(Observations{
		SMBIOSChassisTypes: []uint16{31},
		Capabilities: Capabilities{
			ConvertibleSlate: SlateModeLaptop,
			KeyboardAttached: No,
			Touch:            Yes,
		},
	})
	if profile.FormFactor != FormFactorConvertible {
		t.Fatalf("form factor = %q, want convertible", profile.FormFactor)
	}
	if profile.Capabilities.ConvertibleSlate != SlateModeLaptop || profile.Capabilities.KeyboardAttached != No {
		t.Fatalf("dynamic posture/keyboard state was not preserved: %#v", profile.Capabilities)
	}
}

func TestClassifyConflictingSpecificEvidenceFailsClosed(t *testing.T) {
	profile := Classify(Observations{
		DeviceForm:         FormFactorTablet,
		SMBIOSChassisTypes: []uint16{9},
	})
	if profile.FormFactor != FormFactorUnknown || profile.Confidence != ConfidenceLow {
		t.Fatalf("conflicting strong evidence did not fail closed: %#v", profile)
	}
	if len(profile.Warnings) == 0 {
		t.Fatal("expected a conflict warning")
	}
}

func TestClassifyConflictingSMBIOSCodesFailsClosed(t *testing.T) {
	profile := Classify(Observations{SMBIOSChassisTypes: []uint16{30, 9}})
	if profile.FormFactor != FormFactorUnknown || profile.Confidence != ConfidenceLow {
		t.Fatalf("conflicting SMBIOS codes did not fail closed: %#v", profile)
	}
	if !containsWarning(profile.Warnings, "SMBIOS reporta categorías de chasis incompatibles; se requiere revisión.") {
		t.Fatalf("missing conflict warning: %#v", profile.Warnings)
	}
}

func TestClassifyAllInOneWithGenericDesktopCorroboration(t *testing.T) {
	profile := Classify(Observations{
		SMBIOSChassisTypes: []uint16{13},
		PCSystemType:       PCSystemTypeDesktop,
		Capabilities:       Capabilities{Touch: Yes},
	})
	if profile.FormFactor != FormFactorAllInOne || profile.Confidence != ConfidenceHigh {
		t.Fatalf("unexpected all-in-one classification: %#v", profile)
	}
}

func TestClassifyVirtualEnvironmentDoesNotInferHost(t *testing.T) {
	profile := Classify(Observations{
		SMBIOSChassisTypes: []uint16{6},
		Capabilities: Capabilities{
			VirtualMachine: Yes,
			DedicatedGPU:   Unknown,
		},
	})
	if profile.FormFactor != FormFactorVirtual || profile.Confidence != ConfidenceHigh {
		t.Fatalf("unexpected virtual profile: %#v", profile)
	}
	if len(profile.Warnings) == 0 {
		t.Fatal("expected warning that host hardware is not inferred")
	}
}

func TestClassifyPreservesUnknownCapabilities(t *testing.T) {
	profile := Classify(Observations{})
	if profile.FormFactor != FormFactorUnknown || profile.Confidence != ConfidenceLow {
		t.Fatalf("empty observations must remain unknown: %#v", profile)
	}
	if profile.Capabilities.BatteryPresent != Unknown || profile.Capabilities.Touch != Unknown ||
		profile.Capabilities.DedicatedGPU != Unknown || profile.Capabilities.PowerSource != PowerSourceUnknown {
		t.Fatalf("unavailable capabilities must remain unknown: %#v", profile.Capabilities)
	}
}

func TestClassifyUnknownChassisCodeIsNonFatal(t *testing.T) {
	profile := Classify(Observations{SMBIOSChassisTypes: []uint16{999}})
	if profile.FormFactor != FormFactorUnknown {
		t.Fatalf("unknown chassis code produced %q", profile.FormFactor)
	}
	if !containsWarning(profile.Warnings, "SMBIOS ChassisType 999 no está mapeado; se conserva como dato no concluyente.") {
		t.Fatalf("missing warning for unknown chassis code: %#v", profile.Warnings)
	}
}

func TestClassifyIsDeterministicAndDoesNotMutateInput(t *testing.T) {
	input := Observations{
		SMBIOSChassisTypes: []uint16{10, 9},
		Capabilities: Capabilities{
			Touch: Yes,
		},
	}
	original := append([]uint16(nil), input.SMBIOSChassisTypes...)
	first := Classify(input)
	second := Classify(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("classification is not deterministic:\nfirst: %#v\nsecond: %#v", first, second)
	}
	if !reflect.DeepEqual(input.SMBIOSChassisTypes, original) {
		t.Fatalf("Classify mutated input: got %v, want %v", input.SMBIOSChassisTypes, original)
	}
}

func containsWarning(warnings []string, expected string) bool {
	for _, warning := range warnings {
		if warning == expected {
			return true
		}
	}
	return false
}


func TestClassifyMapsDocumentedSMBIOSChassisFamilies(t *testing.T) {
	tests := []struct {
		name       string
		code       uint16
		want       FormFactor
		confidence Confidence
	}{
		{name: "desktop", code: 3, want: FormFactorDesktop, confidence: ConfidenceMedium},
		{name: "low profile desktop", code: 4, want: FormFactorDesktop, confidence: ConfidenceMedium},
		{name: "pizza box", code: 5, want: FormFactorDesktop, confidence: ConfidenceMedium},
		{name: "mini tower", code: 6, want: FormFactorDesktop, confidence: ConfidenceMedium},
		{name: "tower", code: 7, want: FormFactorDesktop, confidence: ConfidenceMedium},
		{name: "laptop", code: 9, want: FormFactorNotebook, confidence: ConfidenceMedium},
		{name: "notebook", code: 10, want: FormFactorNotebook, confidence: ConfidenceMedium},
		{name: "sub notebook", code: 14, want: FormFactorNotebook, confidence: ConfidenceMedium},
		{name: "handheld", code: 11, want: FormFactorHandheld, confidence: ConfidenceMedium},
		{name: "all in one", code: 13, want: FormFactorAllInOne, confidence: ConfidenceMedium},
		{name: "tablet", code: 30, want: FormFactorTablet, confidence: ConfidenceMedium},
		{name: "convertible", code: 31, want: FormFactorConvertible, confidence: ConfidenceMedium},
		{name: "detachable", code: 32, want: FormFactorDetachable, confidence: ConfidenceMedium},
		{name: "generic portable", code: 8, want: FormFactorUnknown, confidence: ConfidenceLow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := Classify(Observations{SMBIOSChassisTypes: []uint16{test.code}})
			if profile.FormFactor != test.want || profile.Confidence != test.confidence {
				t.Fatalf("Classify(chassis=%d) = %s/%s, want %s/%s; warnings=%v",
					test.code, profile.FormFactor, profile.Confidence, test.want, test.confidence, profile.Warnings)
			}
		})
	}
}
