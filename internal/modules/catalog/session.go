package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

const (
	MaxTimestampMetadataBytes int64 = 16_384
	MaxSnapshotMetadataBytes  int64 = 2_000_000
	MaxTargetsMetadataBytes   int64 = 5_000_000
	MaxTargetBytes             int64 = 512 << 20
)

var (
	ErrMetadataSequence = errors.New("secuencia de metadata TUF inválida")
	ErrInvalidTimestamp = errors.New("metadata timestamp TUF inválida")
	ErrInvalidSnapshot  = errors.New("metadata snapshot TUF inválida")
	ErrInvalidTargets   = errors.New("metadata targets TUF inválida")
	ErrInvalidTarget    = errors.New("target TUF inválido")
	ErrTargetNotFound   = errors.New("target TUF no encontrado")
)

type metadataPhase uint8

const (
	phaseRoot metadataPhase = iota
	phaseTimestamp
	phaseSnapshot
	phaseTargets
)

// MetadataSession validates a single ordered TUF refresh. Any failed step
// poisons the session so partially updated TrustedMetadata cannot be reused.
type MetadataSession struct {
	mu      sync.Mutex
	trusted *trustedmetadata.TrustedMetadata
	phase   metadataPhase
	failure error
}

// VerifiedTarget contains only the target path, byte length and SHA-256
// authenticated by the complete root/timestamp/snapshot/targets chain.
type VerifiedTarget struct {
	Path     string
	Length   int64
	SHA256   string
}

func (session *MetadataSession) UpdateTimestamp(raw []byte) error {
	return session.apply(phaseRoot, phaseTimestamp, raw, MaxTimestampMetadataBytes, ErrInvalidTimestamp, func() error {
		_, err := session.trusted.UpdateTimestamp(raw)
		return err
	})
}

func (session *MetadataSession) UpdateSnapshot(raw []byte) error {
	return session.apply(phaseTimestamp, phaseSnapshot, raw, MaxSnapshotMetadataBytes, ErrInvalidSnapshot, func() error {
		_, err := session.trusted.UpdateSnapshot(raw, false)
		return err
	})
}

func (session *MetadataSession) UpdateTargets(raw []byte) error {
	return session.apply(phaseSnapshot, phaseTargets, raw, MaxTargetsMetadataBytes, ErrInvalidTargets, func() error {
		_, err := session.trusted.UpdateTargets(raw)
		return err
	})
}

func (session *MetadataSession) apply(
	expected metadataPhase,
	next metadataPhase,
	raw []byte,
	maxBytes int64,
	failureKind error,
	update func() error,
) error {
	if session == nil {
		return fmt.Errorf("%w: sesión no inicializada", ErrMetadataSequence)
	}
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.failure != nil {
		return session.failure
	}
	if session.trusted == nil || session.phase != expected {
		session.failure = fmt.Errorf("%w: fase actual %d, requerida %d", ErrMetadataSequence, session.phase, expected)
		return session.failure
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		session.failure = fmt.Errorf("%w: tamaño del documento fuera del límite de %d bytes", failureKind, maxBytes)
		return session.failure
	}
	if err := update(); err != nil {
		session.failure = fmt.Errorf("%w: %v", failureKind, err)
		return session.failure
	}
	session.phase = next
	return nil
}

// Target returns an artifact entry only after timestamp, snapshot and top-level
// targets metadata have all passed go-tuf verification. The application still
// has to compare the target with its artifact descriptor and verify local bytes.
func (session *MetadataSession) Target(targetPath string) (VerifiedTarget, error) {
	if session == nil {
		return VerifiedTarget{}, fmt.Errorf("%w: sesión no inicializada", ErrMetadataSequence)
	}
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.failure != nil {
		return VerifiedTarget{}, session.failure
	}
	if session.trusted == nil || session.phase != phaseTargets {
		return VerifiedTarget{}, fmt.Errorf("%w: targets no fue autenticado", ErrMetadataSequence)
	}
	if targetPath == "" || strings.Contains(targetPath, `\\`) || path.IsAbs(targetPath) || path.Clean(targetPath) != targetPath {
		return VerifiedTarget{}, fmt.Errorf("%w: ruta relativa no canónica", ErrInvalidTarget)
	}
	for _, part := range strings.Split(targetPath, "/") {
		if part == "" || part == "." || part == ".." {
			return VerifiedTarget{}, fmt.Errorf("%w: componente de ruta no permitido", ErrInvalidTarget)
		}
	}

	topTargets := session.trusted.Targets[metadata.TARGETS]
	if topTargets == nil {
		return VerifiedTarget{}, fmt.Errorf("%w: rol targets ausente", ErrMetadataSequence)
	}
	target := topTargets.Signed.Targets[targetPath]
	if target == nil {
		return VerifiedTarget{}, ErrTargetNotFound
	}
	if target.Length <= 0 || target.Length > MaxTargetBytes {
		return VerifiedTarget{}, fmt.Errorf("%w: longitud fuera del límite", ErrInvalidTarget)
	}
	digest, ok := target.Hashes["sha256"]
	if !ok || len(digest) != sha256.Size {
		return VerifiedTarget{}, fmt.Errorf("%w: se requiere un SHA-256 de 32 bytes", ErrInvalidTarget)
	}
	return VerifiedTarget{
		Path:   targetPath,
		Length: target.Length,
		SHA256: hex.EncodeToString(digest),
	}, nil
}
