// Package diagnostics writes bounded, local-only technical reports for
// important failures and detects executions that ended without a clean marker.
package diagnostics

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const (
	runLogPrefix      = "soptraz-run-"
	startupLockName   = ".diagnostics-startup.lock"
	maxLogBytes       = 4 << 20
	maxRecordBytes    = 64 << 10
	maxCrashTailBytes = 64 << 10
	maxRunReports     = 30
)

var (
	ErrLoggerClosed = errors.New("logger de diagnósticos cerrado")
	ErrInvalidEvent = errors.New("evento de diagnóstico inválido")

	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|token|access[_-]?token|refresh[_-]?token|access[_-]?key|api[_-]?key|client[_-]?secret|private[_-]?key|secret|authorization)\b(\s*[:=]\s*)[^\s,;]+`)
	urlCredentialsPattern   = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`)
	authorizationPattern    = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:bearer|basic)\s+[^\s,;]+`)
	bearerPattern           = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	querySecretPattern      = regexp.MustCompile(`(?i)([?&](?:password|passwd|token|access[_-]?token|refresh[_-]?token|access[_-]?key|api[_-]?key|secret|signature|sig|auth)=)[^&\s]+`)
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarn     Severity = "WARN"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
	SeverityFatal    Severity = "FATAL"
	SeverityPanic    Severity = "PANIC"
)

type Event struct {
	Severity  Severity
	Component string
	Operation string
	Code      string
	Message   string
	Cause     string
}

type Logger struct {
	mu             sync.Mutex
	file           *os.File
	runLock        *flock.Flock
	path           string
	closed         bool
	hasEvents      bool
	crashOutputSet bool
	recoveredRuns  []string
}

// DefaultDirectory returns the per-user local folder where incident reports
// are stored. It does not create the directory or a report.
func DefaultDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolver carpeta local de diagnósticos: %w", err)
	}
	return filepath.Join(base, "SOPTRAZ", "diagnostics"), nil
}

// OpenDefault stores reports in the current user's local cache directory.
// Reports remain local and are never uploaded by this package.
func OpenDefault(version string) (*Logger, error) {
	directory, err := DefaultDirectory()
	if err != nil {
		return nil, err
	}
	return Open(directory, version)
}

// Open creates a per-execution text report and recovers reports from previous
// executions that exited without writing a clean-shutdown marker.
func Open(directory, version string) (*Logger, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("carpeta de diagnósticos obligatoria")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolver carpeta de diagnósticos: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("crear carpeta de diagnósticos: %w", err)
	}
	if err := validatePrivateDirectory(absolute); err != nil {
		return nil, err
	}

	startupLock, err := acquireStartupLock(absolute)
	if err != nil {
		return nil, err
	}
	defer startupLock.Unlock()

	recovered, err := recoverPreviousRuns(absolute)
	if err != nil {
		return nil, err
	}
	if err := pruneOldRunReports(absolute, maxRunReports-1, recovered); err != nil {
		return nil, err
	}

	file, err := os.CreateTemp(absolute, runLogPrefix+"*.txt")
	if err != nil {
		return nil, fmt.Errorf("crear informe de diagnóstico: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("proteger informe de diagnóstico: %w", err)
	}

	runLock := flock.New(file.Name(), flock.SetPermissions(0o600))
	locked, err := runLock.TryLock()
	if err != nil || !locked {
		_ = file.Close()
		_ = os.Remove(file.Name())
		if err != nil {
			return nil, fmt.Errorf("bloquear informe de diagnóstico: %w", err)
		}
		return nil, fmt.Errorf("no se pudo adquirir el lock del informe de diagnóstico")
	}

	logger := &Logger{
		file:          file,
		runLock:       runLock,
		path:          file.Name(),
		recoveredRuns: recovered,
	}
	if err := logger.writeHeader(version); err != nil {
		_ = runLock.Unlock()
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return logger, nil
}

// Path returns the local text report path for the current process execution.
func (logger *Logger) Path() string {
	if logger == nil {
		return ""
	}
	return logger.path
}

// RecoveredRuns returns the paths of previous reports found without a clean
// shutdown marker. The cause of an unclean exit is deliberately not inferred.
func (logger *Logger) RecoveredRuns() []string {
	if logger == nil {
		return nil
	}
	return append([]string(nil), logger.recoveredRuns...)
}

// ConfigureCrashOutput asks the Go runtime to duplicate fatal panic/runtime
// diagnostics into this execution's report as well as standard error.
func (logger *Logger) ConfigureCrashOutput() error {
	if logger == nil {
		return fmt.Errorf("logger de diagnósticos ausente")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrLoggerClosed
	}
	if err := debug.SetCrashOutput(logger.file, debug.CrashOptions{}); err != nil {
		return fmt.Errorf("configurar salida de fallos fatales: %w", err)
	}
	logger.crashOutputSet = true
	return nil
}

// Record writes only actionable severities. INFO and WARN are intentionally
// ignored so the technician receives a concise failure report, not routine logs.
func (logger *Logger) Record(event Event) error {
	switch event.Severity {
	case SeverityError, SeverityCritical, SeverityFatal, SeverityPanic:
		// Persist actionable diagnostic.
	case SeverityInfo, SeverityWarn:
		return nil
	default:
		return fmt.Errorf("%w: severidad %q", ErrInvalidEvent, event.Severity)
	}
	if logger == nil {
		return fmt.Errorf("logger de diagnósticos ausente")
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrLoggerClosed
	}
	logger.hasEvents = true
	if err := logger.appendEventLocked(event, nil); err != nil {
		return err
	}
	return nil
}

// RecordPanic records an unexpected recovered panic with the current goroutine
// stack. It does not attempt to recover unrecoverable runtime throws.
func (logger *Logger) RecordPanic(component string, recovered any, stack []byte) error {
	if logger == nil {
		return fmt.Errorf("logger de diagnósticos ausente")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrLoggerClosed
	}
	event := Event{
		Severity:  SeverityPanic,
		Component: component,
		Operation: "ejecución",
		Code:      "runtime.panic",
		Message:   "Se recuperó un panic no esperado en el límite de ejecución.",
		Cause:     fmt.Sprint(recovered),
	}
	logger.hasEvents = true
	if err := logger.appendEventLocked(event, stack); err != nil {
		return err
	}
	return nil
}

// Close records an orderly shutdown. If the process is killed or the OS stops
// it before this method runs, the missing marker is detected on the next start.
func (logger *Logger) Close() error {
	if logger == nil {
		return nil
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return nil
	}

	var result error
	if logger.crashOutputSet {
		if err := debug.SetCrashOutput(nil, debug.CrashOptions{}); err != nil {
			result = errors.Join(result, fmt.Errorf("desactivar salida de fallos fatales: %w", err))
			// Preserve the file if the runtime may still hold its crash-output
			// descriptor; deleting it could discard a later fatal diagnostic.
			logger.hasEvents = true
		}
		logger.crashOutputSet = false
	}

	if _, err := fmt.Fprintf(logger.file,
		"\nCIERRE NORMAL: %s\n",
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		result = errors.Join(result, fmt.Errorf("escribir marca de cierre: %w", err))
	} else if err := logger.file.Sync(); err != nil {
		result = errors.Join(result, fmt.Errorf("sincronizar informe de diagnóstico: %w", err))
	}

	removeRoutineReport := !logger.hasEvents && len(logger.recoveredRuns) == 0
	var startupLock *flock.Flock
	if removeRoutineReport {
		var err error
		startupLock, err = acquireStartupLock(filepath.Dir(logger.path))
		if err != nil {
			result = errors.Join(result, fmt.Errorf("proteger limpieza del informe rutinario: %w", err))
			removeRoutineReport = false
		}
	}

	if logger.runLock != nil {
		if err := logger.runLock.Unlock(); err != nil {
			result = errors.Join(result, fmt.Errorf("liberar lock del informe de diagnóstico: %w", err))
			removeRoutineReport = false
		}
	}
	if logger.file != nil {
		if err := logger.file.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("cerrar informe de diagnóstico: %w", err))
			removeRoutineReport = false
		}
	}
	logger.closed = true

	if removeRoutineReport {
		if err := os.Remove(logger.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("retirar informe sin incidencias: %w", err))
		}
	}
	if startupLock != nil {
		if err := startupLock.Unlock(); err != nil {
			result = errors.Join(result, fmt.Errorf("liberar lock de limpieza de diagnósticos: %w", err))
		}
	}
	return result
}

func (logger *Logger) writeHeader(version string) error {
	var builder strings.Builder
	builder.WriteString("SOPTRAZ — INFORME TÉCNICO DE EJECUCIÓN\n")
	builder.WriteString("=====================================\n")
	fmt.Fprintf(&builder, "Inicio UTC: %s\n", time.Now().UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&builder, "Versión: %s\n", cleanField(version))
	fmt.Fprintf(&builder, "Sistema operativo: %s\n", runtime.GOOS)
	fmt.Fprintf(&builder, "Arquitectura: %s\n", runtime.GOARCH)
	fmt.Fprintf(&builder, "Runtime: %s\n", runtime.Version())
	fmt.Fprintf(&builder, "PID: %d\n", os.Getpid())
	builder.WriteString("Datos personales: no se recopilan deliberadamente.\n")
	builder.WriteString("Estado inicial: EJECUCIÓN EN CURSO; la ausencia de cierre normal puede indicar una interrupción.\n")
	if len(logger.recoveredRuns) > 0 {
		fmt.Fprintf(&builder, "\nATENCIÓN: se detectaron %d ejecución(es) anterior(es) sin cierre normal.\n", len(logger.recoveredRuns))
		for _, path := range logger.recoveredRuns {
			fmt.Fprintf(&builder, "Informe previo: %s\n", filepath.Base(path))
		}
		builder.WriteString("La causa de la interrupción anterior no puede determinarse solo con esta marca.\n")
	}
	builder.WriteString("\n")
	if _, err := io.WriteString(logger.file, builder.String()); err != nil {
		return fmt.Errorf("escribir cabecera del informe: %w", err)
	}
	if err := logger.file.Sync(); err != nil {
		return fmt.Errorf("persistir cabecera del informe: %w", err)
	}
	return nil
}

func (logger *Logger) appendEventLocked(event Event, stack []byte) error {
	var builder strings.Builder
	fmt.Fprintf(&builder, "\n[%s] %s\n",
		time.Now().UTC().Format(time.RFC3339Nano),
		event.Severity,
	)
	writeField(&builder, "Componente", event.Component)
	writeField(&builder, "Operación", event.Operation)
	writeField(&builder, "Código", event.Code)
	writeField(&builder, "Descripción", event.Message)
	writeField(&builder, "Causa", event.Cause)

	if len(stack) > 0 {
		builder.WriteString("Pila de llamadas:\n")
		if len(stack) > maxRecordBytes {
			stack = stack[:maxRecordBytes]
		}
		builder.Write(stack)
		if !bytes.HasSuffix(stack, []byte("\n")) {
			builder.WriteByte('\n')
		}
	}
	payload := builder.String()
	if len(payload) > maxRecordBytes {
		payload = payload[:maxRecordBytes] + "\n[Registro truncado por límite de tamaño]\n"
	}
	info, err := logger.file.Stat()
	if err != nil {
		return fmt.Errorf("consultar tamaño del informe: %w", err)
	}
	if info.Size()+int64(len(payload)) > maxLogBytes {
		return fmt.Errorf("informe de diagnóstico alcanzó el límite de %d bytes", maxLogBytes)
	}
	if _, err := io.WriteString(logger.file, payload); err != nil {
		return fmt.Errorf("escribir evento de diagnóstico: %w", err)
	}
	if err := logger.file.Sync(); err != nil {
		return fmt.Errorf("persistir evento de diagnóstico: %w", err)
	}
	return nil
}

func writeField(builder *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(builder, "%s: %s\n", name, cleanField(value))
}

func cleanField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(value)
	value = urlCredentialsPattern.ReplaceAllString(value, "${1}[CREDENCIALES-OCULTAS]@")
	value = authorizationPattern.ReplaceAllString(value, "${1}[OCULTO]")
	value = bearerPattern.ReplaceAllString(value, "Bearer [OCULTO]")
	value = querySecretPattern.ReplaceAllString(value, "${1}[OCULTO]")
	value = secretAssignmentPattern.ReplaceAllString(value, "${1}${2}[OCULTO]")
	if len(value) > 4096 {
		value = value[:4096] + "…"
	}
	return value
}

func validatePrivateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspeccionar carpeta de diagnósticos: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("la carpeta de diagnósticos debe ser un directorio real")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("proteger carpeta de diagnósticos: %w", err)
	}
	return nil
}

func acquireStartupLock(directory string) (*flock.Flock, error) {
	path := filepath.Join(directory, startupLockName)
	lock, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("adquirir lock de inicio de diagnósticos: %w", err)
	}
	return lock, nil
}

func openLockFile(path string) (*flock.Flock, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if createErr != nil && !errors.Is(createErr, os.ErrExist) {
			return nil, fmt.Errorf("crear lock local: %w", createErr)
		}
		if createErr == nil {
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("cerrar lock local: %w", closeErr)
			}
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, fmt.Errorf("inspeccionar lock local: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("el lock local debe ser un archivo regular")
	}
	return flock.New(path, flock.SetPermissions(0o600)), nil
}

func recoverPreviousRuns(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("listar informes anteriores: %w", err)
	}
	var recovered []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, runLogPrefix) || !strings.HasSuffix(name, ".txt") {
			continue
		}
		path := filepath.Join(directory, name)
		info, err := os.Lstat(path)
		if err != nil {
			return recovered, fmt.Errorf("inspeccionar informe anterior %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return recovered, fmt.Errorf("el informe anterior %q no es un archivo regular", name)
		}

		lock, err := openLockFile(path)
		if err != nil {
			return recovered, err
		}
		locked, err := lock.TryLock()
		if err != nil {
			_ = lock.Unlock()
			return recovered, fmt.Errorf("consultar ejecución previa %q: %w", name, err)
		}
		if !locked {
			continue // A different SOPTRAZ process still owns this execution log.
		}

		marker, readErr := readRunEndMarker(path)
		if readErr != nil {
			_ = lock.Unlock()
			return recovered, fmt.Errorf("leer marca de cierre de informe previo %q: %w", name, readErr)
		}
		if err := trimOversizedReport(path, marker); err != nil {
			_ = lock.Unlock()
			return recovered, fmt.Errorf("limitar tamaño de informe previo %q: %w", name, err)
		}
		if marker == "" {
			file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
			if openErr != nil {
				_ = lock.Unlock()
				return recovered, fmt.Errorf("abrir informe previo %q: %w", name, openErr)
			}
			_, writeErr := fmt.Fprintf(file,
				"\n[%s] CRITICAL\nComponente: runtime\nCódigo: runtime.unexpected_shutdown\nDescripción: La ejecución anterior terminó sin registrar un cierre normal.\nCausa: indeterminada; puede corresponder a kill, panic/fatal, corte de energía o terminación del sistema.\nRECUPERACIÓN REGISTRADA: esta interrupción ya fue detectada por un inicio posterior.\n",
				time.Now().UTC().Format(time.RFC3339Nano),
			)
			if writeErr == nil {
				writeErr = file.Sync()
			}
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				_ = lock.Unlock()
				return recovered, fmt.Errorf("anotar terminación inesperada en %q: %w", name, errors.Join(writeErr, closeErr))
			}
			recovered = append(recovered, path)
		}
		if err := lock.Unlock(); err != nil {
			return recovered, fmt.Errorf("liberar lock de informe anterior %q: %w", name, err)
		}
	}
	return recovered, nil
}

func readRunEndMarker(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	start := int64(0)
	if info.Size() > maxCrashTailBytes {
		start = info.Size() - maxCrashTailBytes
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(bufio.NewReader(io.LimitReader(file, maxCrashTailBytes)))
	if err != nil {
		return "", err
	}
	if bytes.Contains(data, []byte("CIERRE NORMAL:")) {
		return "CIERRE NORMAL:", nil
	}
	if bytes.Contains(data, []byte("RECUPERACIÓN REGISTRADA:")) {
		return "RECUPERACIÓN REGISTRADA:", nil
	}
	return "", nil
}

func pruneOldRunReports(directory string, keep int, protectedPaths []string) error {
	protected := make(map[string]struct{}, len(protectedPaths))
	for _, path := range protectedPaths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolver informe recuperado para retención: %w", err)
		}
		protected[filepath.Clean(absolute)] = struct{}{}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("listar informes para retención: %w", err)
	}
	type reportFile struct {
		path    string
		modTime time.Time
	}
	var reports []reportFile
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), runLogPrefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspeccionar informe para retención %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("informe para retención %q no es un archivo regular", entry.Name())
		}
		reports = append(reports, reportFile{path: path, modTime: info.ModTime()})
	}
	if len(reports) <= keep {
		return nil
	}
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].modTime.Equal(reports[j].modTime) {
			return reports[i].path < reports[j].path
		}
		return reports[i].modTime.Before(reports[j].modTime)
	})

	remaining := len(reports)
	for _, report := range reports {
		if remaining <= keep {
			break
		}
		if _, keepForRecovery := protected[filepath.Clean(report.path)]; keepForRecovery {
			continue
		}
		lock, err := openLockFile(report.path)
		if err != nil {
			return err
		}
		locked, err := lock.TryLock()
		if err != nil {
			_ = lock.Unlock()
			return fmt.Errorf("bloquear informe antiguo para retención: %w", err)
		}
		if !locked {
			continue // Never remove a report owned by a running process.
		}
		if err := lock.Unlock(); err != nil {
			return fmt.Errorf("liberar informe antiguo para retención: %w", err)
		}
		// The caller holds the directory startup lock, so no new scanner can
		// race the unlock-to-remove interval.
		if err := os.Remove(report.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("eliminar informe antiguo por retención: %w", err)
		}
		remaining--
	}
	return nil
}

func trimOversizedReport(path, endMarker string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	// Reserve space for a truncation note and, when present, the clean marker.
	if info.Size() <= int64(maxLogBytes)-1024 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	targetSize := int64(maxLogBytes) - 1024
	if err := file.Truncate(targetSize); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := fmt.Fprintf(file,
		"\n[INFORME TRUNCADO: se conservan los primeros %d bytes para limitar el tamaño local.]\n",
		targetSize,
	)
	if writeErr == nil && endMarker != "" {
		_, writeErr = fmt.Fprintf(file, "%s marca preservada tras truncar el informe %s\n",
			endMarker,
			time.Now().UTC().Format(time.RFC3339Nano))
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}
