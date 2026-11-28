package cli

import (
	"fmt"
	"io"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
	platformprofile "github.com/JavierGrecco/SOPTRAZ/internal/platform/deviceprofile"
)

type deviceProfileReport struct {
	CollectionStatus string                        `json:"collection_status"`
	Observations     moduleprofile.Observations    `json:"observations"`
	Profile          moduleprofile.Profile         `json:"profile"`
	SourceErrors     []platformprofile.SourceError `json:"source_errors,omitempty"`
	MutationsApplied bool                          `json:"mutations_applied"`
	ScopeNote        string                        `json:"scope_note"`
}

const deviceProfileScopeNote = "Inventario de solo lectura con cobertura según plataforma: Windows consulta firmware/SMBIOS, alimentación y dispositivos de puntero; Linux consulta DMI y fuentes de alimentación sysfs; macOS consulta la familia del modelo mediante hw.model. Sensores, SMART, capacidades no recolectadas y datos no verificables permanecen como unknown; no se aplican optimizaciones."

func buildDeviceProfileReport(result platformprofile.Result) deviceProfileReport {
	observations := moduleprofile.NormalizeObservations(result.Observations)
	return deviceProfileReport{
		CollectionStatus: deviceCollectionStatus(result),
		Observations:     observations,
		Profile:          moduleprofile.Classify(observations),
		SourceErrors:     result.SourceErrors,
		MutationsApplied: false,
		ScopeNote:        deviceProfileScopeNote,
	}
}

func deviceCollectionStatus(result platformprofile.Result) string {
	if len(result.SourceErrors) == 0 {
		return "collected"
	}
	if hasObservedDeviceData(result.Observations) {
		return "partial"
	}
	return "unavailable"
}

func hasObservedDeviceData(observations moduleprofile.Observations) bool {
	observations = moduleprofile.NormalizeObservations(observations)
	capabilities := observations.Capabilities
	return (observations.DeviceForm != "" && observations.DeviceForm != moduleprofile.FormFactorUnknown) ||
		len(observations.SMBIOSChassisTypes) > 0 ||
		(observations.PCSystemType != "" && observations.PCSystemType != moduleprofile.PCSystemTypeUnknown) ||
		capabilities.BatteryPresent != moduleprofile.Unknown ||
		capabilities.Touch != moduleprofile.Unknown ||
		capabilities.Pen != moduleprofile.Unknown ||
		capabilities.PhysicalKeyboard != moduleprofile.Unknown ||
		capabilities.KeyboardAttached != moduleprofile.Unknown ||
		capabilities.DedicatedGPU != moduleprofile.Unknown ||
		capabilities.ThermalSensors != moduleprofile.Unknown ||
		capabilities.DockPresent != moduleprofile.Unknown ||
		capabilities.VirtualMachine != moduleprofile.Unknown ||
		(capabilities.PowerSource != "" && capabilities.PowerSource != moduleprofile.PowerSourceUnknown) ||
		(capabilities.ConvertibleSlate != "" && capabilities.ConvertibleSlate != moduleprofile.SlateModeUnknown)
}

func runDeviceProfile(args []string, stdout, stderr io.Writer) int {
	if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
		fmt.Fprintln(stderr, "uso: soptraz device-profile [--json]")
		return 2
	}

	report := buildDeviceProfileReport(platformprofile.Collect())
	if len(args) == 2 {
		return writeJSON(stdout, report)
	}
	writeDeviceProfileText(stdout, report)
	return 0
}

func writeDeviceProfileText(w io.Writer, report deviceProfileReport) {
	profile := report.Profile
	fmt.Fprintf(w, "Estado de recolección: %s\n", report.CollectionStatus)
	fmt.Fprintf(w, "Tipo de equipo: %s\n", formFactorLabel(profile.FormFactor))
	fmt.Fprintf(w, "Confianza: %s\n", confidenceLabel(profile.Confidence))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Capacidades observadas:")
	fmt.Fprintf(w, "  Batería: %s\n", triStateLabel(profile.Capabilities.BatteryPresent))
	fmt.Fprintf(w, "  Alimentación: %s\n", powerSourceLabel(profile.Capabilities.PowerSource))
	fmt.Fprintf(w, "  Pantalla táctil: %s\n", triStateLabel(profile.Capabilities.Touch))
	fmt.Fprintf(w, "  Lápiz: %s\n", triStateLabel(profile.Capabilities.Pen))
	fmt.Fprintf(w, "  Teclado físico: %s\n", triStateLabel(profile.Capabilities.PhysicalKeyboard))
	fmt.Fprintf(w, "  Teclado acoplado: %s\n", triStateLabel(profile.Capabilities.KeyboardAttached))
	fmt.Fprintf(w, "  GPU dedicada: %s\n", triStateLabel(profile.Capabilities.DedicatedGPU))
	fmt.Fprintf(w, "  Sensores térmicos: %s\n", triStateLabel(profile.Capabilities.ThermalSensors))
	fmt.Fprintf(w, "  Dock: %s\n", triStateLabel(profile.Capabilities.DockPresent))
	fmt.Fprintf(w, "  Postura convertible: %s\n", slateModeLabel(profile.Capabilities.ConvertibleSlate))

	if len(profile.Evidence) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Evidencia:")
		for _, evidence := range profile.Evidence {
			fmt.Fprintf(w, "  %s = %s (%s)\n", evidence.Source, evidence.Value, evidence.Strength)
		}
	}
	if len(profile.Warnings) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Advertencias:")
		for _, warning := range profile.Warnings {
			fmt.Fprintf(w, "  - %s\n", warning)
		}
	}
	if len(report.SourceErrors) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Fuentes no disponibles:")
		for _, sourceError := range report.SourceErrors {
			fmt.Fprintf(w, "  - %s: %s\n", sourceError.Source, sourceError.Message)
		}
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, report.ScopeNote)
	fmt.Fprintln(w, "Cambios aplicados: no")
}

func formFactorLabel(value moduleprofile.FormFactor) string {
	switch value {
	case moduleprofile.FormFactorDesktop:
		return "Desktop"
	case moduleprofile.FormFactorNotebook:
		return "Notebook"
	case moduleprofile.FormFactorTablet:
		return "Tablet"
	case moduleprofile.FormFactorConvertible:
		return "Convertible"
	case moduleprofile.FormFactorDetachable:
		return "Detachable"
	case moduleprofile.FormFactorAllInOne:
		return "All-in-one"
	case moduleprofile.FormFactorWorkstation:
		return "Workstation"
	case moduleprofile.FormFactorHandheld:
		return "Handheld"
	case moduleprofile.FormFactorVirtual:
		return "Virtual"
	default:
		return "Desconocido"
	}
}

func confidenceLabel(value moduleprofile.Confidence) string {
	switch value {
	case moduleprofile.ConfidenceHigh:
		return "Alta"
	case moduleprofile.ConfidenceMedium:
		return "Media"
	default:
		return "Baja"
	}
}

func triStateLabel(value moduleprofile.TriState) string {
	switch value {
	case moduleprofile.Yes:
		return "Sí"
	case moduleprofile.No:
		return "No"
	default:
		return "Desconocido"
	}
}

func powerSourceLabel(value moduleprofile.PowerSource) string {
	switch value {
	case moduleprofile.PowerSourceAC:
		return "Corriente alterna"
	case moduleprofile.PowerSourceBattery:
		return "Batería"
	default:
		return "Desconocido"
	}
}

func slateModeLabel(value moduleprofile.SlateMode) string {
	switch value {
	case moduleprofile.SlateModeSlate:
		return "Tablet / teclado no disponible"
	case moduleprofile.SlateModeLaptop:
		return "Notebook / teclado disponible"
	default:
		return "Desconocido"
	}
}
