package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/JavierGrecco/SOPTRAZ/internal/strictjson"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

const (
	MaxTimestampMetadataBytes  int64 = 16_384
	MaxSnapshotMetadataBytes   int64 = 2_000_000
	MaxTargetsMetadataBytes    int64 = 5_000_000
	MaxTargetBytes             int64 = 512 << 20
	MaxArtifactDescriptorBytes int64 = 1 << 20
)

var (
	ErrMetadataSequence         = errors.New("secuencia de metadata TUF inválida")
	ErrInvalidTimestamp         = errors.New("metadata timestamp TUF inválida")
	ErrInvalidSnapshot          = errors.New("metadata snapshot TUF inválida")
	ErrInvalidTargets           = errors.New("metadata targets TUF inválida")
	ErrInvalidTarget            = errors.New("target TUF inválido")
	ErrTargetNotFound           = errors.New("target TUF no encontrado")
	ErrInvalidTargetDescriptor  = errors.New("descriptor artifact v1 inválido en metadata TUF")
	ErrTargetDescriptorMismatch = errors.New("descriptor artifact v1 no coincide con el target TUF")
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
	Path   string
	Length int64
	SHA256 string
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

// decodeArtifactDescriptor applies SOPTRAZ's strict JSON policy before
// decoding the exact artifact v1 contract embedded in TUF target custom metadata.
func decodeArtifactDescriptor(raw json.RawMessage) (artifact.Descriptor, error) {
	var descriptor artifact.Descriptor
	if len(raw) == 0 || int64(len(raw)) > MaxArtifactDescriptorBytes {
		return descriptor, fmt.Errorf("%w: custom ausente o fuera del límite de %d bytes", ErrInvalidTargetDescriptor, MaxArtifactDescriptorBytes)
	}
	if _, err := strictjson.Decode(raw); err != nil {
		return descriptor, fmt.Errorf("%w: JSON estricto: %v", ErrInvalidTargetDescriptor, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return artifact.Descriptor{}, fmt.Errorf("%w: %v", ErrInvalidTargetDescriptor, err)
	}
	if err := descriptor.Validate(); err != nil {
		return artifact.Descriptor{}, fmt.Errorf("%w: %v", ErrInvalidTargetDescriptor, err)
	}
	return descriptor, nil
}

// targetLocked retrieves a target while the caller holds session.mu.
func (session *MetadataSession) targetLocked(targetPath string) (VerifiedTarget, *metadata.TargetFiles, error) {
	if session.failure != nil {
		return VerifiedTarget{}, nil, session.failure
	}
	if session.trusted == nil || session.phase != phaseTargets {
		return VerifiedTarget{}, nil, fmt.Errorf("%w: targets no fue autenticado", ErrMetadataSequence)
	}
	if err := artifact.ValidateRelativePath(targetPath); err != nil {
		return VerifiedTarget{}, nil, fmt.Errorf("%w: %v", ErrInvalidTarget, err)
	}

	topTargets := session.trusted.Targets[metadata.TARGETS]
	if topTargets == nil {
		return VerifiedTarget{}, nil, fmt.Errorf("%w: rol targets ausente", ErrMetadataSequence)
	}
	target := topTargets.Signed.Targets[targetPath]
	if target == nil {
		return VerifiedTarget{}, nil, ErrTargetNotFound
	}
	if target.Length <= 0 || target.Length > MaxTargetBytes {
		return VerifiedTarget{}, nil, fmt.Errorf("%w: longitud fuera del límite", ErrInvalidTarget)
	}
	digest, ok := target.Hashes["sha256"]
	if !ok || len(digest) != sha256.Size {
		return VerifiedTarget{}, nil, fmt.Errorf("%w: se requiere un SHA-256 de 32 bytes", ErrInvalidTarget)
	}
	return VerifiedTarget{
		Path:   targetPath,
		Length: target.Length,
		SHA256: hex.EncodeToString(digest),
	}, target, nil
}

// Target returns an artifact entry only after timestamp, snapshot and top-level
// targets metadata have passed go-tuf verification. It does not by itself prove
// that the complete artifact v1 descriptor is bound to that target; use
// VerifyDescriptor before resolving or considering the artifact installable.
func (session *MetadataSession) Target(targetPath string) (VerifiedTarget, error) {
	if session == nil {
		return VerifiedTarget{}, fmt.Errorf("%w: sesión no inicializada", ErrMetadataSequence)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	verified, _, err := session.targetLocked(targetPath)
	return verified, err
}

// VerifyDescriptor requires the TUF-signed target custom metadata to contain
// the exact artifact v1 descriptor. TUF's signed target length and SHA-256 must
// also match the descriptor. This binds version, platform, architecture,
// license, provenance, path, size and digest to the authenticated target.
func verifySignedArtifactTarget(targetPath string, target *metadata.TargetFiles, descriptor artifact.Descriptor) (VerifiedTarget, error) {
	if err := descriptor.Validate(); err != nil {
		return VerifiedTarget{}, fmt.Errorf("%w: %v", ErrInvalidTargetDescriptor, err)
	}
	if err := artifact.ValidateRelativePath(targetPath); err != nil {
		return VerifiedTarget{}, fmt.Errorf("%w: %v", ErrInvalidTarget, err)
	}
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
	verified := VerifiedTarget{
		Path:   targetPath,
		Length: target.Length,
		SHA256: hex.EncodeToString(digest),
	}
	if target.Custom == nil {
		return VerifiedTarget{}, fmt.Errorf("%w: el target no publica custom artifact v1", ErrInvalidTargetDescriptor)
	}
	signedDescriptor, err := decodeArtifactDescriptor(*target.Custom)
	if err != nil {
		return VerifiedTarget{}, err
	}
	if signedDescriptor != descriptor || verified.Length != descriptor.SizeBytes || verified.SHA256 != descriptor.SHA256 {
		return VerifiedTarget{}, ErrTargetDescriptorMismatch
	}
	return verified, nil
}

// VerifyDescriptor verifies the top-level target and binds its complete artifact
// descriptor to the TUF-signed custom metadata.
func (session *MetadataSession) VerifyDescriptor(descriptor artifact.Descriptor) (VerifiedTarget, error) {
	if session == nil {
		return VerifiedTarget{}, fmt.Errorf("%w: sesión no inicializada", ErrMetadataSequence)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	_, target, err := session.targetLocked(descriptor.RelativePath)
	if err != nil {
		return VerifiedTarget{}, err
	}
	return verifySignedArtifactTarget(descriptor.RelativePath, target, descriptor)
}
