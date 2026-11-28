package diagnostics

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesPrivatePlainTextReportAndCleanCloseMarker(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	logger, err := Open(directory, "test-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	path := logger.Path()
	if filepath.Ext(path) != ".txt" {
		t.Fatalf("report extension = %q, want .txt", filepath.Ext(path))
	}
	if err := logger.Record(Event{
		Severity:  SeverityCritical,
		Component: "catalog.tuf",
		Operation: "refresh",
		Code:      "tuf.metadata.invalid",
		Message:   "No se pudo validar metadata firmada.",
		Cause:     "threshold no satisfecho",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := logger.Record(Event{Severity: SeverityInfo, Message: "routine info"}); err != nil {
		t.Fatalf("Record(INFO) error = %v", err)
	}
	if err := logger.Record(Event{Severity: SeverityWarn, Message: "routine warning"}); err != nil {
		t.Fatalf("Record(WARN) error = %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	report := string(raw)
	for _, expected := range []string{
		"SOPTRAZ — INFORME TÉCNICO DE EJECUCIÓN",
		"Versión: test-version",
		"Sistema operativo:",
		"Arquitectura:",
		"CRITICAL",
		"tuf.metadata.invalid",
		"threshold no satisfecho",
		"CIERRE NORMAL:",
	} {
		if !strings.Contains(report, expected) {
			t.Errorf("report does not contain %q:\n%s", expected, report)
		}
	}
	for _, excluded := range []string{"routine info", "routine warning"} {
		if strings.Contains(report, excluded) {
			t.Errorf("report unexpectedly contains routine event %q", excluded)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat report: %v", err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("report permissions = %o, want no group/other access", info.Mode().Perm())
		}
	}
}

func TestImportantEventRedactsCommonSecretForms(t *testing.T) {
	logger, err := Open(t.TempDir(), "test-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer logger.Close()

	if err := logger.Record(Event{
		Severity: SeverityError,
		Component: "integration.http",
		Code: "http.request.failed",
		Message: "token=abc123 password:supersecret",
		Cause: "https://user:password@example.test/path?api_key=private",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	raw, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	report := string(raw)
	for _, secret := range []string{"abc123", "supersecret", "bearer-secret", "user:password", "api_key=private", "access_token=private-token"} {
		if strings.Contains(report, secret) {
			t.Errorf("report leaked secret %q: %s", secret, report)
		}
	}
	for _, marker := range []string{"[OCULTO]", "[CREDENCIALES-OCULTAS]"} {
		if !strings.Contains(report, marker) {
			t.Errorf("report lacks redaction marker %q: %s", marker, report)
		}
	}
}

func TestDiagnosticsAbruptExitHelper(t *testing.T) {
	if os.Getenv("SOPTRAZ_DIAGNOSTICS_HELPER") != "1" {
		return
	}
	directory := os.Getenv("SOPTRAZ_DIAGNOSTICS_DIR")
	readyPath := os.Getenv("SOPTRAZ_DIAGNOSTICS_READY")
	logger, err := Open(directory, "helper-version")
	if err != nil {
		os.Exit(91)
	}
	if err := logger.Record(Event{
		Severity: SeverityCritical,
		Component: "test.helper",
		Code: "test.abrupt_exit",
		Message: "El proceso de prueba terminará sin cierre normal.",
	}); err != nil {
		os.Exit(92)
	}
	if err := os.WriteFile(readyPath, []byte(logger.Path()), 0o600); err != nil {
		os.Exit(93)
	}
	// The parent forcibly kills this process; Close() and deferred cleanup never run.
	select {}
	runtime.KeepAlive(logger)
}

func TestNextStartupDetectsAbruptProcessTermination(t *testing.T) {
	directory := t.TempDir()
	readyPath := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestDiagnosticsAbruptExitHelper$")
	command.Env = append(os.Environ(),
		"SOPTRAZ_DIAGNOSTICS_HELPER=1",
		"SOPTRAZ_DIAGNOSTICS_DIR="+directory,
		"SOPTRAZ_DIAGNOSTICS_READY="+readyPath,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start abrupt-exit helper: %v", err)
	}
	processExited := false
	defer func() {
		if !processExited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("diagnostics helper did not create its run report in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill diagnostics helper: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("forcibly terminated diagnostics helper exited successfully")
	}
	processExited = true

	logger, err := Open(directory, "recovery-version")
	if err != nil {
		t.Fatalf("Open() after abrupt exit: %v", err)
	}
	defer logger.Close()

	recovered := logger.RecoveredRuns()
	if len(recovered) != 1 {
		t.Fatalf("RecoveredRuns() = %v, want exactly one incomplete execution", recovered)
	}
	previous, err := os.ReadFile(recovered[0])
	if err != nil {
		t.Fatalf("read previous execution report: %v", err)
	}
	report := string(previous)
	for _, expected := range []string{
		"test.abrupt_exit",
		"CRITICAL",
		"runtime.unexpected_shutdown",
		"terminó sin registrar un cierre normal",
		"Causa: indeterminada",
		"RECUPERACIÓN REGISTRADA:",
	} {
		if !strings.Contains(report, expected) {
			t.Errorf("recovered report does not contain %q: %s", expected, report)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close recovery run: %v", err)
	}
	next, err := Open(directory, "third-run-version")
	if err != nil {
		t.Fatalf("Open() after recovery marker: %v", err)
	}
	defer next.Close()
	if len(next.RecoveredRuns()) != 0 {
		t.Fatalf("same interrupted run was reported again: %v", next.RecoveredRuns())
	}
}

func TestConcurrentActiveExecutionIsNotReportedAsCrash(t *testing.T) {
	directory := t.TempDir()
	first, err := Open(directory, "first-version")
	if err != nil {
		t.Fatalf("open first execution: %v", err)
	}
	defer first.Close()

	second, err := Open(directory, "second-version")
	if err != nil {
		t.Fatalf("open concurrent second execution: %v", err)
	}
	defer second.Close()

	if got := len(second.RecoveredRuns()); got != 0 {
		t.Fatalf("second execution classified %d active report(s) as crashes", got)
	}
	raw, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatalf("read first report: %v", err)
	}
	if strings.Contains(string(raw), "runtime.unexpected_shutdown") {
		t.Fatalf("active first execution was incorrectly marked as crashed: %s", raw)
	}
}

func TestRecordRejectsUnknownSeverity(t *testing.T) {
	logger, err := Open(t.TempDir(), "test-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer logger.Close()
	if err := logger.Record(Event{Severity: "DEBUG", Message: "not supported"}); err == nil {
		t.Fatal("Record(DEBUG) succeeded, want invalid severity error")
	}
}

func TestRoutineExecutionDoesNotLeaveAnEmptyReport(t *testing.T) {
	logger, err := Open(t.TempDir(), "test-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	path := logger.Path()
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("routine execution left an empty report behind; stat error = %v", err)
	}
}

func TestPanicReportIncludesStack(t *testing.T) {
	logger, err := Open(t.TempDir(), "test-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer logger.Close()

	stack := []byte("goroutine 1 [running]:\nexample.com/soptraz/failingOperation()\n")
	if err := logger.RecordPanic("test.component", "unexpected panic", stack); err != nil {
		t.Fatalf("RecordPanic() error = %v", err)
	}
	raw, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("read panic report: %v", err)
	}
	for _, expected := range []string{"PANIC", "unexpected panic", "goroutine 1", "failingOperation"} {
		if !strings.Contains(string(raw), expected) {
			t.Errorf("panic report does not contain %q: %s", expected, raw)
		}
	}
}

func TestRetentionDoesNotDeleteReportsRecoveredDuringCurrentStartup(t *testing.T) {
	directory := t.TempDir()
	recoveredPath := filepath.Join(directory, runLogPrefix+"00-interrupted.txt")
	if err := os.WriteFile(recoveredPath,
		[]byte("SOPTRAZ — INFORME TÉCNICO DE EJECUCIÓN\n[CRITICAL] simulated prior crash\n"),
		0o600,
	); err != nil {
		t.Fatalf("create interrupted report: %v", err)
	}
	for index := 0; index < maxRunReports-1; index++ {
		path := filepath.Join(directory, fmt.Sprintf("%s%02d-clean.txt", runLogPrefix, index+1))
		if err := os.WriteFile(path,
			[]byte("SOPTRAZ — INFORME TÉCNICO DE EJECUCIÓN\nCIERRE NORMAL: fixture\n"),
			0o600,
		); err != nil {
			t.Fatalf("create clean report %d: %v", index, err)
		}
	}

	logger, err := Open(directory, "recovery-retention-test")
	if err != nil {
		t.Fatalf("Open() recovery: %v", err)
	}
	defer logger.Close()
	if len(logger.RecoveredRuns()) != 1 || filepath.Clean(logger.RecoveredRuns()[0]) != filepath.Clean(recoveredPath) {
		t.Fatalf("recovered reports = %v, want %q", logger.RecoveredRuns(), recoveredPath)
	}
	if _, err := os.Stat(recoveredPath); err != nil {
		t.Fatalf("recovered report was removed by retention: %v", err)
	}
}

func TestReportRetentionKeepsOnlyMostRecentThirtyRuns(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < maxRunReports+3; index++ {
		logger, err := Open(directory, "retention-test")
		if err != nil {
			t.Fatalf("Open() run %d: %v", index, err)
		}
		if err := logger.Record(Event{
			Severity: SeverityError,
			Component: "test.retention",
			Code: "test.retention",
			Message: "retention fixture",
		}); err != nil {
			t.Fatalf("Record() run %d: %v", index, err)
		}
		if err := logger.Close(); err != nil {
			t.Fatalf("Close() run %d: %v", index, err)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), runLogPrefix) && strings.HasSuffix(entry.Name(), ".txt") {
			count++
		}
	}
	if count != maxRunReports {
		t.Fatalf("retained reports = %d, want %d", count, maxRunReports)
	}
}

func TestDiagnosticsFatalCrashHelper(t *testing.T) {
	if os.Getenv("SOPTRAZ_DIAGNOSTICS_FATAL_HELPER") != "1" {
		return
	}
	directory := os.Getenv("SOPTRAZ_DIAGNOSTICS_DIR")
	logger, err := Open(directory, "fatal-helper-version")
	if err != nil {
		os.Exit(91)
	}
	if err := logger.ConfigureCrashOutput(); err != nil {
		os.Exit(92)
	}
	go func() {
		panic("soptraz-runtime-crash-output-probe")
	}()
	select {}
	runtime.KeepAlive(logger)
}

func TestGoRuntimeFatalPanicIsCopiedToTextReport(t *testing.T) {
	directory := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestDiagnosticsFatalCrashHelper$")
	command.Env = append(os.Environ(),
		"SOPTRAZ_DIAGNOSTICS_FATAL_HELPER=1",
		"SOPTRAZ_DIAGNOSTICS_DIR="+directory,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("fatal helper process exited successfully; expected an unhandled goroutine panic")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	found := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), runLogPrefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatalf("read runtime crash report: %v", err)
		}
		if strings.Contains(string(raw), "soptraz-runtime-crash-output-probe") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Go runtime crash message was not copied to a TXT report; stderr=%s", output)
	}
}

func TestStartupBoundsOversizedRuntimeReportAndPreservesCleanMarker(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, runLogPrefix+"oversized.txt")
	var builder strings.Builder
	builder.WriteString("SOPTRAZ — INFORME TÉCNICO DE EJECUCIÓN\n")
	builder.WriteString("fatal error: simulated runtime dump\n")
	builder.WriteString(strings.Repeat("stack frame diagnostic data\n", (maxLogBytes/20)+100))
	builder.WriteString("CIERRE NORMAL: previous clean shutdown\n")
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write oversized report fixture: %v", err)
	}

	logger, err := Open(directory, "recovery-version")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer logger.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bounded report: %v", err)
	}
	if info.Size() > maxLogBytes {
		t.Fatalf("bounded report size = %d, want <= %d", info.Size(), maxLogBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bounded report: %v", err)
	}
	for _, expected := range []string{"fatal error: simulated runtime dump", "INFORME TRUNCADO", "CIERRE NORMAL: marca preservada"} {
		if !strings.Contains(string(raw), expected) {
			t.Errorf("bounded report does not contain %q", expected)
		}
	}
	if len(logger.RecoveredRuns()) != 0 {
		t.Fatalf("clean oversized report was incorrectly classified as a crash: %v", logger.RecoveredRuns())
	}
}
