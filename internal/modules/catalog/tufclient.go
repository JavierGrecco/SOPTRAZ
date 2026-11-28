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

	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	tufconfig "github.com/theupdateframework/go-tuf/v2/metadata/config"
	tufupdater "github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

var (
	ErrInvalidTUFClientConfig = errors.New("configuración del cliente TUF inválida")
	ErrTUFClientUnavailable   = errors.New("cliente TUF no inicializado")
)

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
}

// persistentTUFTrust keeps the durable bootstrap identity and latest verified
// root used to seed each new official go-tuf Updater.
type persistentTUFTrust struct {
	store          *sqlitetuf.Store
	repositoryID   string
	rootState      sqlitetuf.RootState
	latestRootJSON []byte
	capture        *rootMetadataCaptureTransport
}

// TUFClient wraps the official go-tuf Updater. The Updater is not safe for
// concurrent use, so all refresh/lookup operations are serialized here.
type TUFClient struct {
	mu              sync.Mutex
	config          *tufconfig.UpdaterConfig
	updater         *tufupdater.Updater
	persistence     *persistentTUFTrust
	resolvedTargets map[string]struct{}
	maxDelegations  int
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
		return nil, fmt.Errorf("%w: registrar el ancla persistente: %v", ErrInvalidTUFClientConfig, err)
	}

	records, storedState, err := store.LoadRootChain(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("%w: cargar la cadena de raíces persistida: %v", ErrInvalidTUFClientConfig, err)
	}
	currentRootJSON, replayedState, err := replayStoredRootChain(bootstrapRootJSON, bootstrapState, records)
	if err != nil {
		return nil, fmt.Errorf("%w: no se pudo autenticar la cadena de raíces persistida: %v", ErrInvalidTUFClientConfig, err)
	}
	if replayedState.Version != storedState.Version || replayedState.SHA256 != storedState.SHA256 {
		return nil, fmt.Errorf("%w: el high-water persistido no coincide con la cadena autenticada", ErrInvalidTUFClientConfig)
	}

	persistence := &persistentTUFTrust{
		store:          store,
		repositoryID:   repositoryID,
		rootState:      sqlitetuf.RootState{Version: replayedState.Version, SHA256: replayedState.SHA256},
		latestRootJSON: append([]byte(nil), currentRootJSON...),
	}
	return newTUFClientFromRoot(currentRootJSON, clientConfig, persistence)
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
		config:          cfg,
		persistence:     persistence,
		resolvedTargets: make(map[string]struct{}),
		maxDelegations:  maxDelegations,
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
		if err := client.persistGenerationLocked(client.updater); err != nil {
			delete(client.resolvedTargets, descriptor.RelativePath)
			client.updater = nil
			return VerifiedTarget{}, fmt.Errorf("persistir generación autenticada para %q: %w", descriptor.RelativePath, err)
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
		client.persistence.capture.reset()
	}
	officialUpdater, err := tufupdater.New(client.config)
	if err != nil {
		client.updater = nil
		return fmt.Errorf("inicializar updater TUF oficial: %w", err)
	}

	refreshErr := officialUpdater.Refresh()
	if client.persistence != nil {
		return client.finishPersistentRefresh(officialUpdater, refreshErr)
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

	// Do not commit a captured candidate merely because a second replay accepts
	// it. First prove that the official updater consumed and accepted the same
	// root sequence. This prevents persisting a response that go-tuf rejected
	// earlier (for example because its HTTP Content-Length was invalid).
	trusted := officialUpdater.GetTrustedMetadataSet()
	if trusted.Root == nil {
		if refreshErr != nil {
			return fmt.Errorf("actualizar metadata TUF: %w", refreshErr)
		}
		return fmt.Errorf("%w: updater TUF no devolvió una raíz confiable", ErrTUFClientUnavailable)
	}
	if trusted.Root.Signed.Version != latestState.Version {
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
			return fmt.Errorf("persistir cadena de raíces TUF verificada: %w", err)
		}
		expected := sqlitetuf.RootState{Version: latestState.Version, SHA256: latestState.SHA256}
		if committed != expected {
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
	if err := client.persistGenerationLocked(officialUpdater); err != nil {
		return fmt.Errorf("persistir generación TUF autenticada: %w", err)
	}
	client.updater = officialUpdater
	return nil
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
