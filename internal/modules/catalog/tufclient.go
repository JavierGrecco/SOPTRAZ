// Package catalog provides the local trust boundary for authenticated TUF metadata.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/diagnostics"
	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	tufconfig "github.com/theupdateframework/go-tuf/v2/metadata/config"
	tufupdater "github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

var (
	ErrInvalidTUFClientConfig = errors.New("configuración del cliente TUF inválida")
	ErrTUFClientUnavailable   = errors.New("cliente TUF no inicializado")
	ErrTUFRepositoryInUse     = errors.New("el repositorio TUF ya está siendo utilizado por otra instancia")
)

// TUFDiagnosticRecorder receives selected TUF trust and persistence incidents.
// Implementations must support concurrent calls; recording is best-effort.
type TUFDiagnosticRecorder interface {
	Record(diagnostics.Event) error
}

// TUFClientConfig defines bounded access to a TUF repository. The initial root
// is never read from this config: it must come from a TrustAnchor that was
// pinned or enrolled through a separately verified channel.
type TUFClientConfig struct {
	MetadataURL      string
	TargetsURL       string
	MetadataCacheDir string
	TargetCacheDir   string
	RequestTimeout   time.Duration
	RetryInterval    time.Duration
	RetryCount       uint
	MaxRootRotations int64
	MaxDelegations   int
	// AllowInsecureLoopbackHTTP is intended for local deterministic tests only.
	// Plain HTTP to non-loopback hosts is always rejected.
	AllowInsecureLoopbackHTTP bool
	// Diagnostics is optional. Network availability failures and expected
	// descriptor errors are not reported as trust incidents.
	Diagnostics TUFDiagnosticRecorder
}

// persistentTUFTrust keeps the durable bootstrap identity and latest verified
// root used to seed each new official go-tuf Updater.
type persistentTUFTrust struct {
	store          *sqlitetuf.Store
	repositoryID   string
	rootState      sqlitetuf.RootState
	latestRootJSON []byte
	capture        *rootMetadataCaptureTransport
	repositoryLock tufRepositoryLock
}

// TUFClient wraps the official go-tuf Updater. The Updater is not safe for
// concurrent use, so all refresh/lookup operations are serialized here.
type TUFClient struct {
	mu                sync.Mutex
	config            *tufconfig.UpdaterConfig
	updater           *tufupdater.Updater
	persistence       *persistentTUFTrust
	resolvedTargets   map[string]struct{}
	metadataCacheRoot string
	metadataStageDir  string
	diagnostics       TUFDiagnosticRecorder
}


func recordTUFDiagnostic(
	recorder TUFDiagnosticRecorder,
	severity diagnostics.Severity,
	code string,
	operation string,
	message string,
	cause string,
) {
	if recorder == nil {
		return
	}
	// Never include repository URLs, local paths, raw metadata or provider errors.
	// Diagnostics is best-effort and must not replace the original TUF failure.
	_ = recorder.Record(diagnostics.Event{
		Severity:  severity,
		Component: "catalog.tuf",
		Operation: operation,
		Code:      code,
		Message:   message,
		Cause:     cause,
	})
}

// NewTUFClient builds a bounded production-mode TUF client from a previously
// pinned TrustAnchor and completes one online TUF refresh before returning. The
// verified metadata cache is enabled; UnsafeLocalMode is never enabled. This
// constructor keeps root rotations in memory only for compatibility; use
// NewPersistentTUFClient when a durable root-chain store is available.
func NewTUFClient(anchor *TrustAnchor, clientConfig TUFClientConfig) (*TUFClient, error) {
	if anchor == nil {
		return nil, fmt.Errorf("%w: ancla ausente", ErrInvalidTUFClientConfig)
	}
	rootJSON, err := anchor.rootSnapshot()
	if err != nil {
		return nil, fmt.Errorf("%w: no se pudo obtener root.json confiable: %v", ErrInvalidTUFClientConfig, err)
	}
	return newTUFClientFromRoot(rootJSON, clientConfig, nil)
}

// NewPersistentTUFClient creates a client whose root trust survives process
// restarts. The initial bootstrap pin remains authoritative: every persisted
// rotation is replayed and reverified by go-tuf before it can seed a new
// Updater. The caller owns store and must keep it open for the client's lifetime.
func NewPersistentTUFClient(
	anchor *TrustAnchor,
	clientConfig TUFClientConfig,
	store *sqlitetuf.Store,
	repositoryID string,
) (*TUFClient, error) {
	if anchor == nil {
		return nil, fmt.Errorf("%w: ancla ausente", ErrInvalidTUFClientConfig)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: almacén SQLite ausente", ErrInvalidTUFClientConfig)
	}
	if err := validateRepositoryURL(clientConfig.MetadataURL, clientConfig.AllowInsecureLoopbackHTTP); err != nil {
		return nil, fmt.Errorf("%w: MetadataURL: %v", ErrInvalidTUFClientConfig, err)
	}

	metadataCacheRoot, err := prepareCacheDirectory(clientConfig.MetadataCacheDir)
	if err != nil {
		return nil, fmt.Errorf("%w: MetadataCacheDir: %v", ErrInvalidTUFClientConfig, err)
	}
	repositoryLock, err := acquirePersistentTUFRepositoryLock(store, repositoryID)
	if err != nil {
		return nil, err
	}
	lockTransferred := false
	defer func() {
		if !lockTransferred {
			_ = repositoryLock.Unlock()
		}
	}()
	if _, err := cleanupOrphanedTUFStages(metadataCacheRoot, repositoryLock, repositoryID); err != nil {
		return nil, fmt.Errorf("recuperar stages TUF anteriores: %w", err)
	}

	bootstrapRootJSON, bootstrapState, err := anchor.initialRootSnapshot()
	if err != nil {
		return nil, fmt.Errorf("%w: no se pudo obtener el root bootstrap: %v", ErrInvalidTUFClientConfig, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.RegisterRepository(ctx, sqlitetuf.RepositoryConfig{
		ID:            repositoryID,
		MetadataURL:   clientConfig.MetadataURL,
		AnchorVersion: bootstrapState.Version,
		AnchorSHA256:  bootstrapState.SHA256,
	}); err != nil {
		if errors.Is(err, sqlitetuf.ErrRepositoryConflict) {
			recordTUFDiagnostic(clientConfig.Diagnostics, diagnostics.SeverityCritical, "tuf.repository_binding_conflict", "bootstrap", "La identidad o el ancla del repositorio persistido no coincide con la configuración confiable.", "conflicto de identidad del repositorio")
		} else if errors.Is(err, sqlitetuf.ErrInvalidTrustStore) {
			recordTUFDiagnostic(clientConfig.Diagnostics, diagnostics.SeverityError, "tuf.trust_store_registration_failed", "bootstrap", "No se pudo registrar el repositorio en el almacén SQLite de confianza.", "fallo del almacén local")
		}
		return nil, fmt.Errorf("%w: registrar el ancla persistente: %v", ErrInvalidTUFClientConfig, err)
	}

	records, storedState, err := store.LoadRootChain(ctx, repositoryID)
	if err != nil {
		if errors.Is(err, sqlitetuf.ErrInvalidTrustStore) {
			recordTUFDiagnostic(clientConfig.Diagnostics, diagnostics.SeverityError, "tuf.trust_store_read_failed", "bootstrap", "No se pudo leer de forma consistente el estado de confianza persistido.", "fallo de lectura SQLite")
		}
		return nil, fmt.Errorf("%w: cargar la cadena de raíces persistida: %v", ErrInvalidTUFClientConfig, err)
	}
	currentRootJSON, replayedState, err := replayStoredRootChain(bootstrapRootJSON, bootstrapState, records)
	if err != nil {
		recordTUFDiagnostic(clientConfig.Diagnostics, diagnostics.SeverityCritical, "tuf.root_chain_replay_rejected", "bootstrap", "El almacén contiene una cadena de raíces que no puede autenticarse desde el pin bootstrap.", "cadena de confianza persistida inválida")
		return nil, fmt.Errorf("%w: no se pudo autenticar la cadena de raíces persistida: %v", ErrInvalidTUFClientConfig, err)
	}
	if replayedState.Version != storedState.Version || replayedState.SHA256 != storedState.SHA256 {
		recordTUFDiagnostic(clientConfig.Diagnostics, diagnostics.SeverityCritical, "tuf.root_high_water_mismatch", "bootstrap", "El high-water persistido no coincide con la cadena de raíces autenticada.", "estado anti-rollback inconsistente")
		return nil, fmt.Errorf("%w: el high-water persistido no coincide con la cadena autenticada", ErrInvalidTUFClientConfig)
	}

	persistence := &persistentTUFTrust{
		store:          store,
		repositoryID:   repositoryID,
		rootState:      sqlitetuf.RootState{Version: replayedState.Version, SHA256: replayedState.SHA256},
		latestRootJSON: append([]byte(nil), currentRootJSON...),
		repositoryLock: repositoryLock,
	}
	client, err := newTUFClientFromRoot(currentRootJSON, clientConfig, persistence)
	if err != nil {
		return nil, err
	}
	lockTransferred = true
	return client, nil
}

// newTUFClientFromRoot contains the common bounded updater configuration.
// startRootJSON is already trusted by either the process-local anchor or a
// full replay from the independently pinned bootstrap root.
func newTUFClientFromRoot(
	startRootJSON []byte,
	clientConfig TUFClientConfig,
	persistence *persistentTUFTrust,
) (*TUFClient, error) {
	if len(startRootJSON) == 0 || int64(len(startRootJSON)) > MaxRootMetadataBytes {
		return nil, fmt.Errorf("%w: root.json de inicio inválido", ErrInvalidTUFClientConfig)
	}
	if err := validateRepositoryURL(clientConfig.MetadataURL, clientConfig.AllowInsecureLoopbackHTTP); err != nil {
		return nil, fmt.Errorf("%w: MetadataURL: %v", ErrInvalidTUFClientConfig, err)
	}

	requestTimeout := clientConfig.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = 15 * time.Second
	}
	if requestTimeout < time.Second || requestTimeout > 2*time.Minute {
		return nil, fmt.Errorf("%w: RequestTimeout debe estar entre 1s y 2m", ErrInvalidTUFClientConfig)
	}

	retryCount := clientConfig.RetryCount
	if retryCount == 0 {
		retryCount = 1
	}
	if retryCount > 3 {
		return nil, fmt.Errorf("%w: RetryCount no puede superar 3 intentos", ErrInvalidTUFClientConfig)
	}
	retryInterval := clientConfig.RetryInterval
	if retryInterval < 0 || retryInterval > 5*time.Second {
		return nil, fmt.Errorf("%w: RetryInterval debe estar entre 0 y 5s", ErrInvalidTUFClientConfig)
	}

	maxRootRotations := clientConfig.MaxRootRotations
	if maxRootRotations == 0 {
		maxRootRotations = 32
	}
	if maxRootRotations < 1 || maxRootRotations > 256 {
		return nil, fmt.Errorf("%w: MaxRootRotations debe estar entre 1 y 256", ErrInvalidTUFClientConfig)
	}
	maxDelegations := clientConfig.MaxDelegations
	if maxDelegations == 0 {
		maxDelegations = 32
	}
	if maxDelegations < 1 || maxDelegations > 128 {
		return nil, fmt.Errorf("%w: MaxDelegations debe estar entre 1 y 128", ErrInvalidTUFClientConfig)
	}

	metadataCache, err := prepareCacheDirectory(clientConfig.MetadataCacheDir)
	if err != nil {
		return nil, fmt.Errorf("%w: MetadataCacheDir: %v", ErrInvalidTUFClientConfig, err)
	}
	targetCache, err := prepareCacheDirectory(clientConfig.TargetCacheDir)
	if err != nil {
		return nil, fmt.Errorf("%w: TargetCacheDir: %v", ErrInvalidTUFClientConfig, err)
	}
	if pathsOverlap(metadataCache, targetCache) {
		return nil, fmt.Errorf("%w: las carpetas de metadata y targets deben ser independientes", ErrInvalidTUFClientConfig)
	}

	cfg, err := tufconfig.New(clientConfig.MetadataURL, startRootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: crear configuración go-tuf: %v", ErrInvalidTUFClientConfig, err)
	}
	cfg.LocalMetadataDir = metadataCache
	cfg.LocalTargetsDir = targetCache
	cfg.DisableLocalCache = false
	cfg.UnsafeLocalMode = false
	cfg.MaxRootRotations = maxRootRotations
	cfg.MaxDelegations = maxDelegations
	cfg.RootMaxLength = MaxRootMetadataBytes
	cfg.TimestampMaxLength = MaxTimestampMetadataBytes
	cfg.SnapshotMaxLength = MaxSnapshotMetadataBytes
	cfg.TargetsMaxLength = MaxTargetsMetadataBytes

	if clientConfig.TargetsURL != "" {
		if err := validateRepositoryURL(clientConfig.TargetsURL, clientConfig.AllowInsecureLoopbackHTTP); err != nil {
			return nil, fmt.Errorf("%w: TargetsURL: %v", ErrInvalidTUFClientConfig, err)
		}
		cfg.RemoteTargetsURL = clientConfig.TargetsURL
	}
	httpClient := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// Metadata and target URLs must remain on the explicitly configured
			// origin. Redirects could otherwise move a trusted fetch to another host.
			return http.ErrUseLastResponse
		},
	}
	if persistence != nil {
		capture, err := newRootMetadataCaptureTransport(clientConfig.MetadataURL, httpClient.Transport)
		if err != nil {
			return nil, fmt.Errorf("%w: configurar captura de rotación TUF: %v", ErrInvalidTUFClientConfig, err)
		}
		httpClient.Transport = capture
		persistence.capture = capture
	}
	if err := cfg.SetDefaultFetcherHTTPClient(httpClient); err != nil {
		return nil, fmt.Errorf("%w: configurar cliente HTTP TUF: %v", ErrInvalidTUFClientConfig, err)
	}
	if err := cfg.SetDefaultFetcherRetry(retryInterval, retryCount); err != nil {
		return nil, fmt.Errorf("%w: configurar reintentos TUF: %v", ErrInvalidTUFClientConfig, err)
	}

	client := &TUFClient{
		config:            cfg,
		persistence:       persistence,
		resolvedTargets:   make(map[string]struct{}),
		metadataCacheRoot: metadataCache,
		diagnostics:       clientConfig.Diagnostics,
	}
	if err := client.refreshLocked(); err != nil {
		return nil, err
	}
	return client, nil
}

// ResolveDescriptor asks the official go-tuf Updater to locate and authenticate
// the target, including delegated roles, then compares the full artifact v1
// descriptor with the target's signed custom metadata. It does not download the
// binary, install it, or execute it.
func (client *TUFClient) ResolveDescriptor(descriptor artifact.Descriptor) (VerifiedTarget, error) {
	if client == nil {
		return VerifiedTarget{}, ErrTUFClientUnavailable
	}
	if err := descriptor.Validate(); err != nil {
		return VerifiedTarget{}, fmt.Errorf("%w: %v", ErrInvalidTargetDescriptor, err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.updater == nil {
		return VerifiedTarget{}, ErrTUFClientUnavailable
	}

	target, err := client.updater.GetTargetInfo(descriptor.RelativePath)
	if err != nil {
		return VerifiedTarget{}, fmt.Errorf("resolver de metadata TUF para %q: %w", descriptor.RelativePath, err)
	}
	verified, err := verifySignedArtifactTarget(descriptor.RelativePath, target, descriptor)
	if err != nil {
		return VerifiedTarget{}, err
	}
	if client.resolvedTargets == nil {
		client.resolvedTargets = make(map[string]struct{})
	}
	client.resolvedTargets[descriptor.RelativePath] = struct{}{}
	if client.persistence != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, persistErr := client.commitVerifiedGenerationLocked(ctx)
		cancel()
		if persistErr != nil {
			delete(client.resolvedTargets, descriptor.RelativePath)
			client.updater = nil
			if errors.Is(persistErr, ErrGenerationCapture) {
				recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityCritical, "tuf.generation_verification_rejected", "commit-generation", "Los bytes de metadata no pudieron reproducirse contra el estado autenticado de go-tuf.", "divergencia del estado de metadata")
			} else {
				recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityError, "tuf.generation_commit_failed", "commit-generation", "No se pudo confirmar la generación autenticada en el almacén SQLite.", "fallo de persistencia local")
			}
			return VerifiedTarget{}, fmt.Errorf("persistir generación autenticada para %q: %w", descriptor.RelativePath, persistErr)
		}
	}
	return verified, nil
}

// Refresh creates a new official Updater from the same pinned root and
// performs its single allowed refresh. A failed refresh invalidates the old
// in-memory updater so ResolveDescriptor cannot silently use stale metadata.
// Delegated roles are resolved on demand from the refreshed snapshot.
func (client *TUFClient) Refresh() error {
	if client == nil {
		return ErrTUFClientUnavailable
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.refreshLocked()
}

// refreshLocked must be called with client.mu held. go-tuf's Updater permits
// Refresh only once per instance, so each refresh creates a new instance and
// lets go-tuf revalidate the persisted metadata cache against the trusted root.
func (client *TUFClient) refreshLocked() error {
	client.updater = nil
	client.resolvedTargets = make(map[string]struct{})
	if client.config == nil {
		client.updater = nil
		return ErrTUFClientUnavailable
	}
	if client.persistence != nil {
		if err := client.ensurePersistentRepositoryLockLocked(); err != nil {
			return fmt.Errorf("adquirir exclusividad del repositorio TUF: %w", err)
		}
		if err := client.preparePersistentMetadataStage(); err != nil {
			return fmt.Errorf("preparar caché aislada de metadata TUF: %w", err)
		}
		client.persistence.capture.reset()
	}
	officialUpdater, err := tufupdater.New(client.config)
	if err != nil {
		client.updater = nil
		if client.persistence != nil {
			_ = client.discardPersistentMetadataStage()
		}
		return fmt.Errorf("inicializar updater TUF oficial: %w", err)
	}

	refreshErr := officialUpdater.Refresh()
	if client.persistence != nil {
		if err := client.finishPersistentRefresh(officialUpdater, refreshErr); err != nil {
			_ = client.discardPersistentMetadataStage()
			return err
		}
		return nil
	}

	// Legacy process-local mode: preserve the latest root verified by go-tuf
	// even when a later timestamp/snapshot/targets step fails.
	trusted := officialUpdater.GetTrustedMetadataSet()
	if trusted.Root == nil {
		client.updater = nil
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %w", refreshErr)
		}
		return fmt.Errorf("%w: updater TUF no devolvió una raíz confiable", ErrTUFClientUnavailable)
	}
	rootJSON, rootErr := trusted.Root.ToBytes(false)
	if rootErr != nil {
		client.updater = nil
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %v; conservar raíz verificada: %w", refreshErr, rootErr)
		}
		return fmt.Errorf("serializar raíz TUF verificada: %w", rootErr)
	}
	client.config.LocalTrustedRoot = append([]byte(nil), rootJSON...)

	if refreshErr != nil {
		client.updater = nil
		return fmt.Errorf("actualizar metadata TUF: %w", refreshErr)
	}
	client.updater = officialUpdater
	return nil
}

// finishPersistentRefresh records every exact root response that go-tuf has
// accepted, before propagating a later timestamp/snapshot/targets failure.
func (client *TUFClient) finishPersistentRefresh(
	officialUpdater *tufupdater.Updater,
	refreshErr error,
) error {
	client.updater = nil
	persistence := client.persistence
	if persistence == nil || persistence.store == nil || persistence.capture == nil {
		return ErrTUFClientUnavailable
	}

	candidates, captureErr := persistence.capture.take()
	if captureErr != nil {
		recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityError, "tuf.root_capture_failed", "refresh", "No se pudieron capturar de forma íntegra los bytes de root.json para su persistencia.", "fallo de captura de respuesta")
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %v; capturar root para persistencia: %w", refreshErr, captureErr)
		}
		return fmt.Errorf("capturar root TUF para persistencia: %w", captureErr)
	}

	currentState := TrustState{
		Version: persistence.rootState.Version,
		SHA256:  persistence.rootState.SHA256,
	}
	verifiedRoots, latestRootJSON, latestState, verifyErr := verifyCapturedRootRotations(
		persistence.latestRootJSON,
		currentState,
		candidates,
	)
	if verifyErr != nil {
		recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityCritical, "tuf.root_rotation_rejected", "refresh", "Una root recibida no satisface la secuencia o la verificación criptográfica requerida.", "rotación de raíz rechazada")
	}

	// Do not commit a captured candidate merely because a second replay accepts
	// it. First prove that the official updater consumed and accepted the same
	// root sequence. This prevents persisting a response that go-tuf rejected
	// earlier (for example because its HTTP Content-Length was invalid).
	trusted := officialUpdater.GetTrustedMetadataSet()
	if trusted.Root == nil {
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %w", refreshErr)
		}
		recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityCritical, "tuf.updater_root_missing", "refresh", "go-tuf no devolvió una raíz confiable después del refresh.", "invariante de confianza incumplida")
		return fmt.Errorf("%w: updater TUF no devolvió una raíz confiable", ErrTUFClientUnavailable)
	}
	if trusted.Root.Signed.Version != latestState.Version {
		recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityCritical, "tuf.updater_root_mismatch", "refresh", "La versión root aceptada por go-tuf no coincide con la cadena candidata capturada.", "divergencia entre updater y transporte")
		return fmt.Errorf("%w: go-tuf quedó en root v%d y la cadena candidata en root v%d",
			ErrTUFClientUnavailable, trusted.Root.Signed.Version, latestState.Version)
	}

	if len(verifiedRoots) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		committed, err := persistence.store.CommitVerifiedRoots(
			ctx,
			persistence.repositoryID,
			persistence.rootState,
			verifiedRoots,
		)
		cancel()
		if err != nil {
			recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityError, "tuf.root_persist_failed", "refresh", "No se pudo confirmar la rotación de raíces verificada en SQLite.", "fallo de persistencia local")
			return fmt.Errorf("persistir cadena de raíces TUF verificada: %w", err)
		}
		expected := sqlitetuf.RootState{Version: latestState.Version, SHA256: latestState.SHA256}
		if committed != expected {
			recordTUFDiagnostic(client.diagnostics, diagnostics.SeverityCritical, "tuf.root_commit_mismatch", "refresh", "SQLite devolvió un estado distinto del high-water de raíces verificado.", "invariante de commit incumplida")
			return fmt.Errorf("%w: SQLite confirmó %#v pero se esperaba %#v",
				ErrTUFClientUnavailable, committed, expected)
		}
		persistence.rootState = committed
		persistence.latestRootJSON = append([]byte(nil), latestRootJSON...)
		client.config.LocalTrustedRoot = append([]byte(nil), latestRootJSON...)
	}

	if verifyErr != nil {
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %v; validar/persistir rotación: %w", refreshErr, verifyErr)
		}
		return fmt.Errorf("validar/persistir rotación TUF: %w", verifyErr)
	}
	if refreshErr != nil {
		return fmt.Errorf("actualizar metadata TUF: %w", refreshErr)
	}
	client.updater = officialUpdater
	return nil
}

// ensurePersistentRepositoryLockLocked reacquires repository exclusivity after
// Close() and reaps stages left by a process that terminated without Close().
func (client *TUFClient) ensurePersistentRepositoryLockLocked() error {
	if client == nil || client.persistence == nil || client.persistence.store == nil {
		return ErrTUFClientUnavailable
	}
	if lock := client.persistence.repositoryLock; lock != nil && lock.Locked() {
		return nil
	}

	lock, err := acquirePersistentTUFRepositoryLock(client.persistence.store, client.persistence.repositoryID)
	if err != nil {
		return err
	}
	if _, err := cleanupOrphanedTUFStages(client.metadataCacheRoot, lock, client.persistence.repositoryID); err != nil {
		_ = lock.Unlock()
		return fmt.Errorf("limpiar stages TUF abandonados: %w", err)
	}
	client.persistence.repositoryLock = lock
	return nil
}

// preparePersistentMetadataStage creates a private working directory for one
// updater attempt. Only the last committed SQLite generation is copied into it;
// go-tuf still performs its normal online verification before using any cached
// metadata. A failed refresh is discarded without modifying the previous
// generation or the caller's metadata-cache root.
func (client *TUFClient) preparePersistentMetadataStage() error {
	if client == nil || client.config == nil || client.persistence == nil ||
		client.persistence.store == nil || client.metadataCacheRoot == "" {
		return ErrTUFClientUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	generation, loadErr := client.persistence.store.LoadActiveGeneration(ctx, client.persistence.repositoryID)
	cancel()
	if errors.Is(loadErr, sqlitetuf.ErrGenerationNotFound) ||
		errors.Is(loadErr, sqlitetuf.ErrGenerationRootMismatch) {
		// There is no generation compatible with the currently pinned root.
		// Starting with an empty working cache is safe; online TUF refresh will
		// fetch and authenticate the current metadata.
		generation.Roles = nil
		loadErr = nil
	}
	if loadErr != nil {
		return fmt.Errorf("%w: recuperar generación activa para el caché: %v", ErrGenerationCapture, loadErr)
	}

	stageDir, err := os.MkdirTemp(client.metadataCacheRoot, tufMetadataStagePrefixForRepository(client.persistence.repositoryID)+"*")
	if err != nil {
		return fmt.Errorf("crear directorio temporal privado: %w", err)
	}
	if err := os.Chmod(stageDir, 0o700); err != nil {
		_ = os.RemoveAll(stageDir)
		return fmt.Errorf("proteger directorio temporal: %w", err)
	}

	for _, role := range generation.Roles {
		if role.Role == "" || role.Role == "." || role.Role == ".." || len(role.Metadata) == 0 {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("%w: rol de generación inválido al restaurar el caché", ErrGenerationCapture)
		}
		filename := url.PathEscape(role.Role) + ".json"
		if filepath.Base(filename) != filename || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("%w: nombre de rol no apto para caché: %q", ErrGenerationCapture, role.Role)
		}
		filePath := filepath.Join(stageDir, filename)
		file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("crear metadata restaurada para %q: %w", role.Role, err)
		}
		_, writeErr := file.Write(role.Metadata)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("escribir metadata restaurada para %q: %w", role.Role, errors.Join(writeErr, closeErr))
		}
	}

	previousStage := client.metadataStageDir
	client.metadataStageDir = stageDir
	client.config.LocalMetadataDir = stageDir
	if previousStage != "" && previousStage != stageDir {
		// The previous updater is no longer used: refreshLocked invalidates it
		// before reaching this method, and client.mu is held throughout.
		_ = os.RemoveAll(previousStage)
	}
	return nil
}

// discardPersistentMetadataStage abandons the current attempt and restores
// the configured cache root as the next staging parent.
func (client *TUFClient) discardPersistentMetadataStage() error {
	if client == nil {
		return nil
	}
	stageDir := client.metadataStageDir
	if stageDir != "" {
		if err := os.RemoveAll(stageDir); err != nil {
			return err
		}
	}
	client.metadataStageDir = ""
	if client.config != nil && client.metadataCacheRoot != "" {
		client.config.LocalMetadataDir = client.metadataCacheRoot
	}
	return nil
}

// Close releases the updater's private metadata workspace. The SQLite store
// remains owned by the caller and is not closed here. Close is safe to call
// more than once; Refresh can create a new workspace afterwards.
func (client *TUFClient) Close() error {
	if client == nil {
		return nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.updater = nil
	client.resolvedTargets = make(map[string]struct{})

	var result error
	if err := client.discardPersistentMetadataStage(); err != nil {
		result = errors.Join(result, fmt.Errorf("limpiar workspace de metadata TUF: %w", err))
	}
	if client.persistence != nil && client.persistence.repositoryLock != nil {
		if err := client.persistence.repositoryLock.Unlock(); err != nil {
			result = errors.Join(result, fmt.Errorf("liberar lock del repositorio TUF: %w", err))
		} else {
			client.persistence.repositoryLock = nil
		}
	}
	return result
}

func (anchor *TrustAnchor) rootSnapshot() ([]byte, error) {
	if anchor == nil {
		return nil, ErrInvalidTrustAnchor
	}
	anchor.mu.RLock()
	defer anchor.mu.RUnlock()
	if len(anchor.rootJSON) == 0 || anchor.trusted == nil || anchor.trusted.Root == nil {
		return nil, ErrInvalidTrustAnchor
	}
	return append([]byte(nil), anchor.rootJSON...), nil
}

func validateRepositoryURL(raw string, allowInsecureLoopback bool) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("se requiere una URL absoluta sin credenciales, query ni fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && allowInsecureLoopback && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("se requiere HTTPS; HTTP solo se admite para loopback cuando está habilitado explícitamente")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func prepareCacheDirectory(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("la carpeta es obligatoria")
	}
	absolute, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("la ruta debe ser una carpeta real, no un symlink")
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return "", err
	}
	return absolute, nil
}

func pathsOverlap(first, second string) bool {
	firstWithinSecond := pathWithin(first, second)
	secondWithinFirst := pathWithin(second, first)
	return firstWithinSecond || secondWithinFirst
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
