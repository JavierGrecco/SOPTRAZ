// Package deviceprofile normalizes hardware evidence into a conservative device
// classification. It does not collect host data or apply system changes.
package deviceprofile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FormFactor describes the physical device category when it can be established
// from hardware-provided evidence. It is not a usage profile.
type FormFactor string

const (
	FormFactorUnknown     FormFactor = "unknown"
	FormFactorDesktop     FormFactor = "desktop"
	FormFactorNotebook    FormFactor = "notebook"
	FormFactorTablet      FormFactor = "tablet"
	FormFactorConvertible FormFactor = "convertible"
	FormFactorDetachable  FormFactor = "detachable"
	FormFactorAllInOne    FormFactor = "all_in_one"
	FormFactorWorkstation FormFactor = "workstation"
	FormFactorHandheld    FormFactor = "handheld"
	FormFactorVirtual     FormFactor = "virtual"
)

// Confidence explains how strongly the available hardware evidence supports
// the classification. Low-confidence results are informational only.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// TriState represents a hardware capability without confusing a failed or
// unavailable query with a negative result.
type TriState uint8

const (
	Unknown TriState = iota
	No
	Yes
)

// PowerSource is dynamic operating context, not a form-factor classifier.
type PowerSource string

const (
	PowerSourceUnknown PowerSource = "unknown"
	PowerSourceAC      PowerSource = "ac"
	PowerSourceBattery PowerSource = "battery"
)

// PCSystemType is the normalized coarse WMI system category. Mobile is
// intentionally not specific enough to distinguish notebooks from tablets.
type PCSystemType string

const (
	PCSystemTypeUnknown     PCSystemType = "unknown"
	PCSystemTypeDesktop     PCSystemType = "desktop"
	PCSystemTypeMobile      PCSystemType = "mobile"
	PCSystemTypeWorkstation PCSystemType = "workstation"
	PCSystemTypeServer      PCSystemType = "server"
)

// SlateMode describes a convertible's current keyboard/posture state. It must
// not be used to change the device's physical form-factor classification.
type SlateMode string

const (
	SlateModeUnknown SlateMode = "unknown"
	SlateModeSlate   SlateMode = "slate"
	SlateModeLaptop  SlateMode = "laptop"
)

// Capabilities holds read-only, tri-state facts and dynamic operating context.
type Capabilities struct {
	BatteryPresent   TriState    `json:"battery_present"`
	Touch            TriState    `json:"touch"`
	Pen              TriState    `json:"pen"`
	PhysicalKeyboard TriState    `json:"physical_keyboard"`
	KeyboardAttached TriState    `json:"keyboard_attached"`
	DedicatedGPU     TriState    `json:"dedicated_gpu"`
	ThermalSensors   TriState    `json:"thermal_sensors"`
	DockPresent      TriState    `json:"dock_present"`
	VirtualMachine   TriState    `json:"virtual_machine"`
	PowerSource      PowerSource `json:"power_source"`
	ConvertibleSlate SlateMode   `json:"convertible_slate_mode"`
}

// Observations contains normalized, read-only results from platform adapters.
// DeviceForm is the normalized Windows DeviceForm result when available.
// SMBIOSChassisTypes contains raw Win32_SystemEnclosure.ChassisTypes codes.
type Observations struct {
	DeviceForm         FormFactor   `json:"device_form,omitempty"`
	SMBIOSChassisTypes []uint16     `json:"smbios_chassis_types,omitempty"`
	PCSystemType       PCSystemType `json:"pc_system_type,omitempty"`
	Capabilities       Capabilities `json:"capabilities"`
}

// Evidence records why a classification was selected or left uncertain.
type Evidence struct {
	Source    string     `json:"source"`
	Value     string     `json:"value"`
	Candidate FormFactor `json:"candidate"`
	Strength  string     `json:"strength"`
}

// Profile is the deterministic result of classifying one observation snapshot.
type Profile struct {
	FormFactor   FormFactor   `json:"form_factor"`
	Confidence   Confidence   `json:"classification_confidence"`
	Capabilities Capabilities `json:"capabilities"`
	Evidence     []Evidence   `json:"classification_evidence"`
	Warnings     []string     `json:"warnings,omitempty"`
}

const (
	evidenceDeviceForm = "windows.device_form"
	evidenceSMBIOS     = "windows.win32_system_enclosure.chassis_types"
	evidencePCSystem   = "windows.win32_computer_system.pc_system_type"
	evidenceVirtual    = "windows.virtual_machine"
)

// Classify turns an observation snapshot into a conservative profile. It is
// intentionally deterministic and never infers tablet/notebook from touch,
// pen, battery, keyboard or slate posture alone.
func Classify(observations Observations) Profile {
	profile := Profile{
		FormFactor:   FormFactorUnknown,
		Confidence:   ConfidenceLow,
		Capabilities: normalizeCapabilities(observations.Capabilities),
		Evidence:     make([]Evidence, 0, 4),
		Warnings:     make([]string, 0, 4),
	}

	if observations.Capabilities.VirtualMachine == Yes {
		profile.FormFactor = FormFactorVirtual
		profile.Confidence = ConfidenceHigh
		profile.Evidence = append(profile.Evidence, Evidence{
			Source: evidenceVirtual, Value: "true", Candidate: FormFactorVirtual, Strength: "explicit",
		})
		if hasPhysicalFormFactorEvidence(observations) {
			profile.Warnings = append(profile.Warnings, "El entorno virtual informa también datos de chasis físico; no se infiere el hardware del host.")
		}
		return finalize(profile)
	}

	deviceForm := observations.DeviceForm
	if deviceForm != "" && deviceForm != FormFactorUnknown {
		if !isSupportedFormFactor(deviceForm) {
			profile.Warnings = append(profile.Warnings, fmt.Sprintf("DeviceForm contiene una categoría no soportada: %q.", deviceForm))
		} else {
			profile.Evidence = append(profile.Evidence, Evidence{
				Source: evidenceDeviceForm, Value: string(deviceForm), Candidate: deviceForm, Strength: "specific",
			})
		}
	}

	chassisCandidates := make(map[FormFactor]struct{})
	chassisCodes := append([]uint16(nil), observations.SMBIOSChassisTypes...)
	sort.Slice(chassisCodes, func(i, j int) bool { return chassisCodes[i] < chassisCodes[j] })
	for _, code := range chassisCodes {
		candidate, recognized, generic := formFactorFromChassis(code)
		if recognized {
			chassisCandidates[candidate] = struct{}{}
			profile.Evidence = append(profile.Evidence, Evidence{
				Source: evidenceSMBIOS, Value: strconv.FormatUint(uint64(code), 10), Candidate: candidate, Strength: "specific",
			})
		} else if generic {
			profile.Evidence = append(profile.Evidence, Evidence{
				Source: evidenceSMBIOS, Value: strconv.FormatUint(uint64(code), 10), Candidate: FormFactorUnknown, Strength: "generic",
			})
			profile.Warnings = append(profile.Warnings, fmt.Sprintf("SMBIOS ChassisType %d es demasiado genérico para distinguir tablet de notebook.", code))
		} else {
			profile.Warnings = append(profile.Warnings, fmt.Sprintf("SMBIOS ChassisType %d no está mapeado; se conserva como dato no concluyente.", code))
		}
	}

	if len(chassisCandidates) > 1 {
		profile.Warnings = append(profile.Warnings, "SMBIOS reporta categorías de chasis incompatibles; se requiere revisión.")
		return finalize(profile)
	}

	var chassisForm FormFactor
	for candidate := range chassisCandidates {
		chassisForm = candidate
	}

	pcForm, pcRecognized := formFactorFromPCSystemType(observations.PCSystemType)
	if observations.PCSystemType != "" && observations.PCSystemType != PCSystemTypeUnknown {
		value := string(observations.PCSystemType)
		candidate := pcForm
		strength := "coarse"
		if !pcRecognized {
			candidate = FormFactorUnknown
			strength = "ambiguous"
		}
		profile.Evidence = append(profile.Evidence, Evidence{
			Source: evidencePCSystem, Value: value, Candidate: candidate, Strength: strength,
		})
		if observations.PCSystemType == PCSystemTypeMobile {
			profile.Warnings = append(profile.Warnings, "PCSystemType Mobile no distingue por sí solo notebook, tablet o convertible.")
		}
	}

	switch {
	case deviceForm != "" && deviceForm != FormFactorUnknown && isSupportedFormFactor(deviceForm):
		if chassisForm != FormFactorUnknown && !samePhysicalFamily(deviceForm, chassisForm) {
			profile.Warnings = append(profile.Warnings, fmt.Sprintf("DeviceForm (%s) contradice SMBIOS (%s); se evita una clasificación automática.", deviceForm, chassisForm))
			return finalize(profile)
		}
		profile.FormFactor = moreSpecificFormFactor(deviceForm, chassisForm)
		profile.Confidence = ConfidenceMedium
		if chassisForm != FormFactorUnknown {
			profile.Confidence = ConfidenceHigh
		}
		if pcRecognized && !compatibleWithPCSystemType(profile.FormFactor, observations.PCSystemType) {
			profile.Confidence = ConfidenceMedium
			profile.Warnings = append(profile.Warnings, "PCSystemType contradice la categoría específica; la clasificación se mantiene con confianza reducida.")
		}
	case chassisForm != FormFactorUnknown:
		profile.FormFactor = chassisForm
		profile.Confidence = ConfidenceMedium
		if compatibleWithPCSystemType(chassisForm, observations.PCSystemType) {
			profile.Confidence = ConfidenceHigh
		} else if pcRecognized {
			profile.Warnings = append(profile.Warnings, "PCSystemType contradice la categoría SMBIOS; se reduce la confianza.")
			profile.Confidence = ConfidenceLow
		}
	case pcRecognized:
		profile.FormFactor = pcForm
		profile.Confidence = ConfidenceLow
	case observations.PCSystemType == PCSystemTypeMobile:
		profile.Warnings = append(profile.Warnings, "No hay evidencia específica suficiente para clasificar este equipo como notebook o tablet.")
	}

	return finalize(profile)
}

func normalizeCapabilities(capabilities Capabilities) Capabilities {
	if capabilities.PowerSource == "" {
		capabilities.PowerSource = PowerSourceUnknown
	}
	if capabilities.ConvertibleSlate == "" {
		capabilities.ConvertibleSlate = SlateModeUnknown
	}
	return capabilities
}

func isSupportedFormFactor(formFactor FormFactor) bool {
	switch formFactor {
	case FormFactorDesktop, FormFactorNotebook, FormFactorTablet,
		FormFactorConvertible, FormFactorDetachable, FormFactorAllInOne,
		FormFactorWorkstation, FormFactorHandheld:
		return true
	default:
		return false
	}
}

func formFactorFromChassis(code uint16) (FormFactor, bool, bool) {
	switch code {
	case 3, 4, 5, 6, 7:
		return FormFactorDesktop, true, false
	case 8:
		return FormFactorUnknown, false, true
	case 9, 10, 14:
		return FormFactorNotebook, true, false
	case 11:
		return FormFactorHandheld, true, false
	case 13:
		return FormFactorAllInOne, true, false
	case 30:
		return FormFactorTablet, true, false
	case 31:
		return FormFactorConvertible, true, false
	case 32:
		return FormFactorDetachable, true, false
	default:
		return FormFactorUnknown, false, false
	}
}

func formFactorFromPCSystemType(systemType PCSystemType) (FormFactor, bool) {
	switch systemType {
	case PCSystemTypeDesktop:
		return FormFactorDesktop, true
	case PCSystemTypeWorkstation:
		return FormFactorWorkstation, true
	default:
		return FormFactorUnknown, false
	}
}

func compatibleWithPCSystemType(formFactor FormFactor, systemType PCSystemType) bool {
	switch systemType {
	case PCSystemTypeDesktop:
		return formFactor == FormFactorDesktop || formFactor == FormFactorAllInOne || formFactor == FormFactorWorkstation
	case PCSystemTypeWorkstation:
		return formFactor == FormFactorDesktop || formFactor == FormFactorWorkstation
	case PCSystemTypeMobile:
		return formFactor == FormFactorNotebook || formFactor == FormFactorTablet ||
			formFactor == FormFactorConvertible || formFactor == FormFactorDetachable ||
			formFactor == FormFactorHandheld
	default:
		return false
	}
}

func samePhysicalFamily(first, second FormFactor) bool {
	if first == second {
		return true
	}
	if first == FormFactorDesktop {
		return second == FormFactorAllInOne || second == FormFactorWorkstation
	}
	if second == FormFactorDesktop {
		return first == FormFactorAllInOne || first == FormFactorWorkstation
	}
	return false
}

func moreSpecificFormFactor(first, second FormFactor) FormFactor {
	if first == FormFactorDesktop && (second == FormFactorAllInOne || second == FormFactorWorkstation) {
		return second
	}
	if second == FormFactorDesktop && (first == FormFactorAllInOne || first == FormFactorWorkstation) {
		return first
	}
	return first
}

func hasPhysicalFormFactorEvidence(observations Observations) bool {
	if observations.DeviceForm != "" && observations.DeviceForm != FormFactorUnknown {
		return true
	}
	return len(observations.SMBIOSChassisTypes) > 0 || observations.PCSystemType != "" && observations.PCSystemType != PCSystemTypeUnknown
}

func finalize(profile Profile) Profile {
	sort.Slice(profile.Evidence, func(i, j int) bool {
		if profile.Evidence[i].Source != profile.Evidence[j].Source {
			return profile.Evidence[i].Source < profile.Evidence[j].Source
		}
		if profile.Evidence[i].Value != profile.Evidence[j].Value {
			return profile.Evidence[i].Value < profile.Evidence[j].Value
		}
		return profile.Evidence[i].Candidate < profile.Evidence[j].Candidate
	})
	sort.Strings(profile.Warnings)
	profile.Warnings = uniqueStrings(profile.Warnings)
	if strings.TrimSpace(string(profile.FormFactor)) == "" {
		profile.FormFactor = FormFactorUnknown
	}
	return profile
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
