package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/buildinfo"
	"github.com/JavierGrecco/SOPTRAZ/internal/contracts"
	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/JavierGrecco/SOPTRAZ/internal/platform/host"
	"github.com/JavierGrecco/SOPTRAZ/internal/platform/trustedtime"
)

const timeStatusNote = "Diagnóstico local de solo lectura. No modifica el equipo. No es evidencia temporal criptográfica, no valida expiración TUF y no autoriza el catálogo offline."

type timeStatusReport struct {
	SchemaVersion        string                             `json:"schema_version"`
	Status               string                             `json:"status"`
	WallClockObservedAt  time.Time                          `json:"wall_clock_observed_at"`
	ServiceState         trustedtime.ServiceState           `json:"service_state"`
	SynchronizationState trustedtime.SynchronizationState   `json:"synchronization_state"`
	Mechanism            trustedtime.TimeMechanism          `json:"mechanism"`
	LastSyncAt           *time.Time                         `json:"last_sync_at,omitempty"`
	Note                 string                             `json:"note"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stdout)
		return 0
	}
	switch args[0] {
	case "help", "--help", "-h":
		writeUsage(stdout)
		return 0
	case "version":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			fmt.Fprintln(stderr, "uso: soptraz version [--json]")
			return 2
		}
		if len(args) == 2 {
			return writeJSON(stdout, buildinfo.Info())
		}
		fmt.Fprintf(stdout, "SOPTRAZ %s
", buildinfo.Version)
		return 0
	case "host-info":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			fmt.Fprintln(stderr, "uso: soptraz host-info [--json]")
			return 2
		}
		info := host.Current()
		if len(args) == 2 {
			return writeJSON(stdout, map[string]string{
				"os":           info.OS,
				"architecture": info.Architecture,
				"go_version":   info.GoVersion,
			})
		}
		fmt.Fprintf(stdout, "Sistema: %s
Arquitectura: %s
Runtime: %s
", info.OS, info.Architecture, info.GoVersion)
		return 0
	case "device-profile":
		return runDeviceProfile(args, stdout, stderr)
	case "time-status":
		return runTimeStatus(args, stdout, stderr)
	case "validate":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "uso: soptraz validate <manifest|action|intervention|event|checkpoint|artifact> <archivo.json>")
			return 2
		}
		raw, err := readContractFile(args[2])
		if err != nil {
			fmt.Fprintf(stderr, "no se pudo leer el contrato: %v
", err)
			return 1
		}
		if err := contracts.ValidateDocument(args[1], raw); err != nil {
			fmt.Fprintf(stderr, "contrato inválido: %v
", err)
			return 1
		}
		if args[1] == "artifact" {
			var descriptor artifact.Descriptor
			if err := json.Unmarshal(raw, &descriptor); err != nil {
				fmt.Fprintf(stderr, "descriptor de artefacto inválido: %v
", err)
				return 1
			}
			if err := descriptor.Validate(); err != nil {
				fmt.Fprintf(stderr, "descriptor de artefacto inválido: %v
", err)
				return 1
			}
		}
		fmt.Fprintf(stdout, "Contrato %s válido: %s
", args[1], args[2])
		return 0
	default:
		fmt.Fprintf(stderr, "comando desconocido: %s

", args[0])
		writeUsage(stderr)
		return 2
	}
}

func runTimeStatus(args []string, stdout, stderr io.Writer) int {
	return runTimeStatusWithProvider(args, stdout, stderr, trustedtime.NewSystemTimeDiagnosticsProvider())
}

func runTimeStatusWithProvider(
	args []string,
	stdout, stderr io.Writer,
	provider trustedtime.SystemTimeDiagnosticsProvider,
) int {
	if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
		fmt.Fprintln(stderr, "uso: soptraz time-status [--json]")
		return 2
	}
	if provider == nil {
		fmt.Fprintln(stderr, "no se pudo obtener el diagnóstico del reloj del sistema")
		return 1
	}

	diagnostics, err := provider.Observe(context.Background())
	status := "available"
	if errors.Is(err, trustedtime.ErrSystemTimeDiagnosticsUnavailable) {
		status = "unavailable"
	} else if err != nil {
		// Keep unexpected adapter details out of the user-facing CLI output.
		fmt.Fprintln(stderr, "no se pudo obtener el diagnóstico del reloj del sistema")
		return 1
	}

	canonical, err := diagnostics.Canonical()
	if err != nil {
		fmt.Fprintln(stderr, "el adaptador devolvió un diagnóstico horario inválido")
		return 1
	}

	report := timeStatusReport{
		SchemaVersion:        "1.0.0",
		Status:               status,
		WallClockObservedAt:  canonical.WallClockObservedAt,
		ServiceState:         canonical.ServiceState,
		SynchronizationState: canonical.SynchronizationState,
		Mechanism:            canonical.Mechanism,
		Note:                 timeStatusNote,
	}
	if !canonical.LastSyncAt.IsZero() {
		lastSync := canonical.LastSyncAt
		report.LastSyncAt = &lastSync
	}

	if len(args) == 2 {
		return writeJSON(stdout, report)
	}

	fmt.Fprintf(stdout, "Hora observada: %s
", report.WallClockObservedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(stdout, "Servicio horario: %s
", serviceStateText(report.ServiceState))
	fmt.Fprintf(stdout, "Sincronización informada: %s
", synchronizationStateText(report.SynchronizationState))
	fmt.Fprintf(stdout, "Mecanismo: %s
", timeMechanismText(report.Mechanism))
	if report.LastSyncAt == nil {
		fmt.Fprintln(stdout, "Última sincronización: desconocida")
	} else {
		fmt.Fprintf(stdout, "Última sincronización: %s
", report.LastSyncAt.Format(time.RFC3339Nano))
	}
	if status == "unavailable" {
		fmt.Fprintln(stdout, "Disponibilidad: no disponible (el estado permanece desconocido)")
	} else {
		fmt.Fprintln(stdout, "Disponibilidad: disponible")
	}
	fmt.Fprintln(stdout, report.Note)
	return 0
}

func serviceStateText(state trustedtime.ServiceState) string {
	switch state {
	case trustedtime.ServiceStateActive:
		return "activo"
	case trustedtime.ServiceStateInactive:
		return "inactivo"
	default:
		return "desconocido"
	}
}

func synchronizationStateText(state trustedtime.SynchronizationState) string {
	switch state {
	case trustedtime.SynchronizationSynchronized:
		return "sincronizado"
	case trustedtime.SynchronizationNotSynchronized:
		return "no sincronizado"
	default:
		return "desconocido"
	}
}

func timeMechanismText(mechanism trustedtime.TimeMechanism) string {
	switch mechanism {
	case trustedtime.TimeMechanismLinuxKernel:
		return "kernel de Linux (adjtimex)"
	case trustedtime.TimeMechanismWindowsW32Time:
		return "Windows Time Service (W32Time)"
	case trustedtime.TimeMechanismSystemdTimedate1:
		return "systemd timedate1"
	case trustedtime.TimeMechanismMacOSTimed:
		return "daemon timed de macOS"
	case trustedtime.TimeMechanismCustom:
		return "adaptador personalizado"
	default:
		return "desconocido"
	}
}

func readContractFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, contracts.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("el archivo supera el límite de %d bytes", contracts.MaxDocumentBytes)
	}
	return raw, nil
}

func writeJSON(w io.Writer, value any) int {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(w, "no se pudo generar la salida JSON: %v
", err)
		return 1
	}
	return 0
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "SOPTRAZ — Intelligent IT Operations")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Uso:")
	fmt.Fprintln(w, "  soptraz help")
	fmt.Fprintln(w, "  soptraz version [--json]")
	fmt.Fprintln(w, "  soptraz host-info [--json]")
	fmt.Fprintln(w, "  soptraz device-profile [--json]")
	fmt.Fprintln(w, "  soptraz time-status [--json]")
	fmt.Fprintln(w, "  soptraz validate <manifest|action|intervention|event|checkpoint|artifact> <archivo.json>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "La CLI inicial solo expone información de lectura y validación. No modifica el equipo.")
}
