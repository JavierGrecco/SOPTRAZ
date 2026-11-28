package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/JavierGrecco/SOPTRAZ/internal/app/cli"
	"github.com/JavierGrecco/SOPTRAZ/internal/buildinfo"
	"github.com/JavierGrecco/SOPTRAZ/internal/diagnostics"
)

func main() {
	os.Exit(run())
}

// run keeps process-level defers alive before main calls os.Exit. It captures
// recoverable panics and configures Go's fatal runtime output as a local text
// report without making diagnostics availability a prerequisite for SOPTRAZ.
func run() (exitCode int) {
	logger, err := diagnostics.OpenDefault(buildinfo.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SOPTRAZ: no se pudo iniciar el registro de diagnósticos local: %v\n", err)
	} else {
		if err := logger.ConfigureCrashOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "SOPTRAZ: no se pudo configurar el registro de fallos fatales: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "SOPTRAZ: informe técnico local: %s\n", logger.Path())
		if recovered := logger.RecoveredRuns(); len(recovered) > 0 {
			fmt.Fprintf(os.Stderr, "SOPTRAZ: se detectó una ejecución anterior sin cierre normal; revisar: %s\n", recovered[0])
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			if logger != nil {
				if recordErr := logger.RecordPanic("cmd/soptraz", recovered, stack); recordErr != nil {
					fmt.Fprintf(os.Stderr, "SOPTRAZ: no se pudo escribir el informe del panic: %v\n", recordErr)
				}
			} else {
				fmt.Fprintf(os.Stderr, "SOPTRAZ: panic no recuperado: %v\n%s", recovered, stack)
			}
			exitCode = 2
		}
		if logger != nil {
			if closeErr := logger.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "SOPTRAZ: no se pudo cerrar el informe técnico: %v\n", closeErr)
			}
		}
	}()

	return cli.Run(os.Args[1:], os.Stdout, os.Stderr)
}
