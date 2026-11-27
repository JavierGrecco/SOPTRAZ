package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JavierGrecco/SOPTRAZ/internal/buildinfo"
	"github.com/JavierGrecco/SOPTRAZ/internal/platform/host"
)

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
		fmt.Fprintf(stdout, "SOPTRAZ %s\n", buildinfo.Version)
		return 0
	case "host-info":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			fmt.Fprintln(stderr, "uso: soptraz host-info [--json]")
			return 2
		}
		info := host.Current()
		if len(args) == 2 {
			return writeJSON(stdout, map[string]string{
				"os": info.OS,
				"architecture": info.Architecture,
				"go_version": info.GoVersion,
			})
		}
		fmt.Fprintf(stdout, "Sistema: %s\nArquitectura: %s\nRuntime: %s\n", info.OS, info.Architecture, info.GoVersion)
		return 0
	default:
		fmt.Fprintf(stderr, "comando desconocido: %s\n\n", args[0])
		writeUsage(stderr)
		return 2
	}
}

func writeJSON(w io.Writer, value any) int {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(w, "no se pudo generar la salida JSON: %v\n", err)
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
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "La CLI inicial solo expone información de lectura. No modifica el equipo.")
}
