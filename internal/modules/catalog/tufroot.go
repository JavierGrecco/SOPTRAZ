// Package catalog provides the local trust boundary for authenticated TUF metadata.
package catalog

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

const MaxRootMetadataBytes int64 = 512_000

var (
	ErrInvalidTrustAnchor = errors.New("ancla de confianza TUF inválida")
	ErrInvalidRootUpdate  = errors.New("actualización de raíz TUF inválida")
)

// TrustState is a read-only snapshot of the currently trusted root metadata.
type TrustState struct {
	Version int64
	SHA256  string
}

// TrustAnchor owns the TUF root trust state. The initial fingerprint must be
// compiled into SOPTRAZ or installed through a separately verified enrollment
// process; it must never be accepted from the catalogue or an untrusted UI.
type TrustAnchor struct {
	mu          sync.RWMutex
	trusted     *trustedmetadata.TrustedMetadata
	rootSHA256  [sha256.Size]byte
	rootVersion int64
}

// NewPinnedTrustAnchor verifies the exact root bytes against a pre-established
// SHA-256 pin, then asks go-tuf to verify the root's type, signatures and
// threshold. A valid self-signature without the independent pin is not trust.
func NewPinnedTrustAnchor(rootJSON []byte, expectedSHA256 string) (*TrustAnchor, error) {
	if len(rootJSON) == 0 || int64(len(rootJSON)) > MaxRootMetadataBytes {
		return nil, fmt.Errorf("%w: tamaño de root.json fuera del límite", ErrInvalidTrustAnchor)
	}
	if len(expectedSHA256) != sha256.Size*2 || expectedSHA256 != strings.ToLower(expectedSHA256) {
		return nil, fmt.Errorf("%w: el pin SHA-256 debe tener 64 caracteres hexadecimales minúsculos", ErrInvalidTrustAnchor)
	}
	expected, err := hex.DecodeString(expectedSHA256)
	if err != nil {
		return nil, fmt.Errorf("%w: pin SHA-256 inválido: %v", ErrInvalidTrustAnchor, err)
	}

	digest := sha256.Sum256(rootJSON)
	if subtle.ConstantTimeCompare(digest[:], expected) != 1 {
		return nil, fmt.Errorf("%w: root.json no coincide con el pin integrado", ErrInvalidTrustAnchor)
	}

	trusted, err := trustedmetadata.New(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: go-tuf rechazó root.json: %v", ErrInvalidTrustAnchor, err)
	}
	if trusted.Root == nil || trusted.Root.Signed.Type != metadata.ROOT || trusted.Root.Signed.Version < 1 {
		return nil, fmt.Errorf("%w: tipo o versión de root.json inválidos", ErrInvalidTrustAnchor)
	}

	return &TrustAnchor{
		trusted:     trusted,
		rootSHA256:  digest,
		rootVersion: trusted.Root.Signed.Version,
	}, nil
}

// State returns the currently trusted root version and the SHA-256 of its exact
// metadata bytes. It does not expose mutable go-tuf structures to callers.
func (anchor *TrustAnchor) State() (TrustState, error) {
	if anchor == nil {
		return TrustState{}, fmt.Errorf("%w: ancla no inicializada", ErrInvalidTrustAnchor)
	}
	anchor.mu.RLock()
	defer anchor.mu.RUnlock()
	if anchor.trusted == nil || anchor.trusted.Root == nil {
		return TrustState{}, fmt.Errorf("%w: ancla no inicializada", ErrInvalidTrustAnchor)
	}
	return TrustState{
		Version: anchor.rootVersion,
		SHA256:  hex.EncodeToString(anchor.rootSHA256[:]),
	}, nil
}

// VerifyAndRotateRoot accepts only the immediately following root version,
// signed by the previously trusted root and by the new root's own threshold.
// It never fetches metadata or persists trust state; atomic durable persistence
// and the offline anti-rollback store belong to the catalogue repository layer.
func (anchor *TrustAnchor) VerifyAndRotateRoot(nextRootJSON []byte) (TrustState, error) {
	if anchor == nil {
		return TrustState{}, fmt.Errorf("%w: ancla no inicializada", ErrInvalidRootUpdate)
	}
	if len(nextRootJSON) == 0 || int64(len(nextRootJSON)) > MaxRootMetadataBytes {
		return TrustState{}, fmt.Errorf("%w: tamaño de root.json fuera del límite", ErrInvalidRootUpdate)
	}

	anchor.mu.Lock()
	defer anchor.mu.Unlock()
	if anchor.trusted == nil || anchor.trusted.Root == nil {
		return TrustState{}, fmt.Errorf("%w: ancla no inicializada", ErrInvalidRootUpdate)
	}

	rotated, err := anchor.trusted.UpdateRoot(nextRootJSON)
	if err != nil {
		return TrustState{}, fmt.Errorf("%w: %v", ErrInvalidRootUpdate, err)
	}
	digest := sha256.Sum256(nextRootJSON)
	anchor.rootSHA256 = digest
	anchor.rootVersion = rotated.Signed.Version
	return TrustState{
		Version: anchor.rootVersion,
		SHA256:  hex.EncodeToString(anchor.rootSHA256[:]),
	}, nil
}
