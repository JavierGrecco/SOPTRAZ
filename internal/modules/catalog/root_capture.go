package catalog

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
)

// capturedRootMetadata is the exact response body returned for a versioned
// TUF root request. It is only a candidate until go-tuf and the persistent
// trust chain independently verify it.
type capturedRootMetadata struct {
	version  int64
	metadata []byte
}

// rootMetadataCaptureTransport observes only successful N.root.json responses
// from the configured metadata origin. It replays the exact bytes to go-tuf,
// so go-tuf remains the protocol verifier and this transport does not replace
// signature, threshold, version or expiry checks.
type rootMetadataCaptureTransport struct {
	base       http.RoundTripper
	origin     *url.URL
	mu         sync.Mutex
	roots      map[int64][]byte
	captureErr error
}

func newRootMetadataCaptureTransport(metadataURL string, base http.RoundTripper) (*rootMetadataCaptureTransport, error) {
	origin, err := url.Parse(metadataURL)
	if err != nil || origin == nil || origin.Host == "" {
		return nil, fmt.Errorf("origen de metadata TUF inválido para captura de raíces")
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &rootMetadataCaptureTransport{
		base:   base,
		origin: origin,
		roots:  make(map[int64][]byte),
	}, nil
}

func (transport *rootMetadataCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	version, ok := transport.requestedRootVersion(request)
	if !ok || response.StatusCode != http.StatusOK || response.Body == nil {
		return response, nil
	}

	// Read at most one byte beyond the protocol limit. The same bounded bytes
	// are then handed back to go-tuf, which independently enforces RootMaxLength.
	metadata, readErr := io.ReadAll(io.LimitReader(response.Body, sqlitetuf.MaxRootMetadataBytes+1))
	closeErr := response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(metadata))
	if readErr != nil {
		transport.recordError(fmt.Errorf("leer root.json v%d para persistencia: %w", version, readErr))
		return response, nil
	}
	if closeErr != nil {
		transport.recordError(fmt.Errorf("cerrar respuesta root.json v%d: %w", version, closeErr))
	}
	if int64(len(metadata)) == 0 || int64(len(metadata)) > sqlitetuf.MaxRootMetadataBytes {
		transport.recordError(fmt.Errorf("root.json v%d excede el límite de %d bytes", version, sqlitetuf.MaxRootMetadataBytes))
		return response, nil
	}
	transport.recordRoot(version, metadata)
	return response, nil
}

func (transport *rootMetadataCaptureTransport) requestedRootVersion(request *http.Request) (int64, bool) {
	if request == nil || request.URL == nil || request.Method != http.MethodGet {
		return 0, false
	}
	if !strings.EqualFold(request.URL.Scheme, transport.origin.Scheme) ||
		!strings.EqualFold(request.URL.Host, transport.origin.Host) {
		return 0, false
	}

	basePath := strings.TrimRight(transport.origin.Path, "/")
	requestPath := request.URL.Path
	var relative string
	if basePath == "" {
		relative = strings.TrimPrefix(requestPath, "/")
	} else {
		prefix := basePath + "/"
		if !strings.HasPrefix(requestPath, prefix) {
			return 0, false
		}
		relative = strings.TrimPrefix(requestPath, prefix)
	}
	if relative == "" || strings.Contains(relative, "/") || path.Base(relative) != relative {
		return 0, false
	}
	versionText := strings.TrimSuffix(relative, ".root.json")
	if versionText == relative {
		return 0, false
	}
	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil || version < 1 || strconv.FormatInt(version, 10) != versionText {
		return 0, false
	}
	return version, true
}

// reset discards candidates from a previous refresh before a new Updater run.
func (transport *rootMetadataCaptureTransport) reset() {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.roots = make(map[int64][]byte)
	transport.captureErr = nil
}

func (transport *rootMetadataCaptureTransport) recordRoot(version int64, metadata []byte) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if previous, ok := transport.roots[version]; ok {
		if !bytes.Equal(previous, metadata) && transport.captureErr == nil {
			transport.captureErr = fmt.Errorf("el repositorio devolvió bytes distintos para root.json v%d", version)
		}
		return
	}
	transport.roots[version] = append([]byte(nil), metadata...)
}

func (transport *rootMetadataCaptureTransport) recordError(err error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.captureErr == nil {
		transport.captureErr = err
	}
}

// take returns a detached, version-sorted snapshot and resets the capture for
// the next refresh. It never exposes mutable response buffers to the caller.
func (transport *rootMetadataCaptureTransport) take() ([]capturedRootMetadata, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	roots := make([]capturedRootMetadata, 0, len(transport.roots))
	for version, metadata := range transport.roots {
		roots = append(roots, capturedRootMetadata{version: version, metadata: append([]byte(nil), metadata...)})
	}
	// The root updater requests versions sequentially. Sorting makes the
	// persistence path defensive even if the transport is later used concurrently.
	for i := 1; i < len(roots); i++ {
		for j := i; j > 0 && roots[j].version < roots[j-1].version; j-- {
			roots[j], roots[j-1] = roots[j-1], roots[j]
		}
	}
	if len(roots) == 0 && transport.captureErr == nil {
		transport.roots = make(map[int64][]byte)
		return roots, nil
	}
	err := transport.captureErr
	transport.roots = make(map[int64][]byte)
	transport.captureErr = nil
	return roots, err
}
