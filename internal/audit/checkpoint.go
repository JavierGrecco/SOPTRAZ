package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	CheckpointSchemaVersion     = "1.0.0"
	CheckpointSignatureAlgorithm = "ed25519"
	checkpointDomainSeparator   = "SOPTRAZ-AUDIT-CHECKPOINT-V1\x00"
	checkpointDigestDomain      = "SOPTRAZ-AUDIT-CHECKPOINT-DIGEST-V1\x00"
	maxCheckpointFieldBytes     = 4096
)

// Checkpoint commits to one immutable head of an intervention audit chain.
// CreatedAt is informational and is not a trusted timestamp.
type Checkpoint struct {
	SchemaVersion         string `json:"schemaVersion"`
	CheckpointID          string `json:"checkpointId"`
	InterventionID        string `json:"interventionId"`
	EventSchemaVersion    string `json:"eventSchemaVersion"`
	HashAlgorithm         string `json:"hashAlgorithm"`
	SignatureAlgorithm    string `json:"signatureAlgorithm"`
	Sequence              uint64 `json:"sequence"`
	HeadHash              string `json:"headHash"`
	PreviousCheckpointHash string `json:"previousCheckpointHash,omitempty"`
	CreatedAt             string `json:"createdAt"`
	KeyID                 string `json:"keyId"`
}

// SignedCheckpoint contains a checkpoint and its Ed25519 signature.
// The private key is never included in this structure or serialized form.
type SignedCheckpoint struct {
	Checkpoint Checkpoint `json:"checkpoint"`
	Signature  string     `json:"signature"`
}

// CheckpointKeyResolver resolves a public key from an independently managed
// trust store. Implementations must not trust keys supplied by the bundle.
type CheckpointKeyResolver interface {
	ResolvePublicKey(ctx context.Context, keyID string) (ed25519.PublicKey, error)
}

// NewCheckpoint creates a version 1 checkpoint using the current audit schema
// and derives the key ID from the supplied public key.
func NewCheckpoint(interventionID string, sequence uint64, headHash, previousCheckpointHash string, publicKey ed25519.PublicKey, createdAt time.Time) (Checkpoint, error) {
	if createdAt.IsZero() {
		return Checkpoint{}, fmt.Errorf("la fecha del checkpoint no puede estar vacía")
	}

	checkpointID, err := NewEventID()
	if err != nil {
		return Checkpoint{}, err
	}
	keyID, err := CheckpointKeyID(publicKey)
	if err != nil {
		return Checkpoint{}, err
	}

	checkpoint := Checkpoint{
		SchemaVersion:          CheckpointSchemaVersion,
		CheckpointID:           checkpointID,
		InterventionID:         interventionID,
		EventSchemaVersion:     SchemaVersion,
		HashAlgorithm:          HashAlgorithm,
		SignatureAlgorithm:     CheckpointSignatureAlgorithm,
		Sequence:               sequence,
		HeadHash:               headHash,
		PreviousCheckpointHash: previousCheckpointHash,
		CreatedAt:              createdAt.UTC().Format(time.RFC3339Nano),
		KeyID:                  keyID,
	}
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// Validate checks all fields that participate in checkpoint signing.
func (checkpoint Checkpoint) Validate() error {
	if checkpoint.SchemaVersion != CheckpointSchemaVersion {
		return fmt.Errorf("versión de checkpoint no compatible %q", checkpoint.SchemaVersion)
	}
	if !isCanonicalUUID(checkpoint.CheckpointID) {
		return fmt.Errorf("checkpointId debe ser un UUID canónico en minúsculas")
	}
	if !isCanonicalUUID(checkpoint.InterventionID) {
		return fmt.Errorf("interventionId debe ser un UUID canónico en minúsculas")
	}
	if checkpoint.EventSchemaVersion != SchemaVersion {
		return fmt.Errorf("versión de evento no compatible %q", checkpoint.EventSchemaVersion)
	}
	if checkpoint.HashAlgorithm != HashAlgorithm {
		return fmt.Errorf("algoritmo de hash no compatible %q", checkpoint.HashAlgorithm)
	}
	if checkpoint.SignatureAlgorithm != CheckpointSignatureAlgorithm {
		return fmt.Errorf("algoritmo de firma no compatible %q", checkpoint.SignatureAlgorithm)
	}
	if checkpoint.Sequence == 0 {
		return fmt.Errorf("la secuencia del checkpoint debe ser mayor que cero")
	}
	if !isCanonicalSHA256(checkpoint.HeadHash) {
		return fmt.Errorf("headHash debe ser un SHA-256 hexadecimal en minúsculas")
	}
	if checkpoint.PreviousCheckpointHash != "" && !isCanonicalSHA256(checkpoint.PreviousCheckpointHash) {
		return fmt.Errorf("previousCheckpointHash debe ser vacío o un SHA-256 hexadecimal en minúsculas")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, checkpoint.CreatedAt)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != checkpoint.CreatedAt || !strings.HasSuffix(checkpoint.CreatedAt, "Z") {
		return fmt.Errorf("createdAt debe ser una fecha RFC3339Nano canónica en UTC")
	}
	if !isCanonicalSHA256(checkpoint.KeyID) {
		return fmt.Errorf("keyId debe ser el SHA-256 hexadecimal en minúsculas de la clave pública")
	}
	return nil
}

// CheckpointKeyID returns the full SHA-256 fingerprint of an Ed25519 public
// key. Trust stores use this stable identifier to resolve the key separately
// from the checkpoint package.
func CheckpointKeyID(publicKey ed25519.PublicKey) (string, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("clave pública Ed25519 inválida: longitud %d", len(publicKey))
	}
	digest := sha256.Sum256(publicKey)
	return hex.EncodeToString(digest[:]), nil
}

// CheckpointSigningBytes returns the version 1 binary signing preimage.
// Fields are length-prefixed and written in a fixed order; JSON whitespace,
// map iteration and field order cannot alter the signature input.
func CheckpointSigningBytes(checkpoint Checkpoint) ([]byte, error) {
	if err := checkpoint.Validate(); err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	buffer.WriteString(checkpointDomainSeparator)

	headerFields := []string{
		checkpoint.SchemaVersion,
		checkpoint.CheckpointID,
		checkpoint.InterventionID,
		checkpoint.EventSchemaVersion,
		checkpoint.HashAlgorithm,
		checkpoint.SignatureAlgorithm,
	}
	for _, field := range headerFields {
		if err := writeCheckpointString(&buffer, field); err != nil {
			return nil, err
		}
	}

	var sequence [8]byte
	binary.BigEndian.PutUint64(sequence[:], checkpoint.Sequence)
	buffer.Write(sequence[:])

	tailFields := []string{
		checkpoint.HeadHash,
		checkpoint.PreviousCheckpointHash,
		checkpoint.CreatedAt,
		checkpoint.KeyID,
	}
	for _, field := range tailFields {
		if err := writeCheckpointString(&buffer, field); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

// SignCheckpoint signs a checkpoint with an explicitly supplied Ed25519
// private key. If KeyID is empty it is derived; otherwise it must match.
func SignCheckpoint(checkpoint Checkpoint, privateKey ed25519.PrivateKey) (SignedCheckpoint, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return SignedCheckpoint{}, fmt.Errorf("clave privada Ed25519 inválida: longitud %d", len(privateKey))
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID, err := CheckpointKeyID(publicKey)
	if err != nil {
		return SignedCheckpoint{}, err
	}
	if checkpoint.KeyID == "" {
		checkpoint.KeyID = keyID
	} else if checkpoint.KeyID != keyID {
		return SignedCheckpoint{}, fmt.Errorf("keyId del checkpoint no corresponde a la clave de firma")
	}

	message, err := CheckpointSigningBytes(checkpoint)
	if err != nil {
		return SignedCheckpoint{}, err
	}
	signature := ed25519.Sign(privateKey, message)
	return SignedCheckpoint{
		Checkpoint: checkpoint,
		Signature:  base64.StdEncoding.EncodeToString(signature),
	}, nil
}

// VerifySignedCheckpoint verifies a checkpoint against a public key already
// obtained from a trusted source. It does not establish trust in that key.
func VerifySignedCheckpoint(signed SignedCheckpoint, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("clave pública Ed25519 inválida: longitud %d", len(publicKey))
	}
	keyID, err := CheckpointKeyID(publicKey)
	if err != nil {
		return err
	}
	if signed.Checkpoint.KeyID != keyID {
		return fmt.Errorf("la clave pública no coincide con el keyId del checkpoint")
	}

	message, err := CheckpointSigningBytes(signed.Checkpoint)
	if err != nil {
		return err
	}
	signature, err := decodeCheckpointSignature(signed.Signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return fmt.Errorf("firma Ed25519 del checkpoint inválida")
	}
	return nil
}

// VerifyCheckpointWithTrustStore resolves the key ID from an independent
// trust store before checking the signature.
func VerifyCheckpointWithTrustStore(ctx context.Context, signed SignedCheckpoint, resolver CheckpointKeyResolver) error {
	if ctx == nil {
		return fmt.Errorf("el contexto de verificación no puede ser nil")
	}
	if resolver == nil {
		return fmt.Errorf("el trust store de checkpoints no puede ser nil")
	}
	if err := signed.Checkpoint.Validate(); err != nil {
		return err
	}
	publicKey, err := resolver.ResolvePublicKey(ctx, signed.Checkpoint.KeyID)
	if err != nil {
		return fmt.Errorf("resolver keyId %q: %w", signed.Checkpoint.KeyID, err)
	}
	return VerifySignedCheckpoint(signed, publicKey)
}

// CheckpointDigest returns the SHA-256 digest used to link the next
// checkpoint to this signed checkpoint. Callers must verify its signature
// against a trusted key before treating the digest as an authenticated anchor.
func CheckpointDigest(signed SignedCheckpoint) (string, error) {
	message, err := CheckpointSigningBytes(signed.Checkpoint)
	if err != nil {
		return "", err
	}
	signature, err := decodeCheckpointSignature(signed.Signature)
	if err != nil {
		return "", err
	}

	var buffer bytes.Buffer
	buffer.WriteString(checkpointDigestDomain)
	if err := writeCheckpointBytes(&buffer, message); err != nil {
		return "", err
	}
	if err := writeCheckpointBytes(&buffer, signature); err != nil {
		return "", err
	}
	digest := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func decodeCheckpointSignature(value string) ([]byte, error) {
	signature, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("signature base64 inválida: %w", err)
	}
	if len(signature) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(signature) != value {
		return nil, fmt.Errorf("signature Ed25519 inválida: longitud o codificación incorrecta")
	}
	return signature, nil
}

func writeCheckpointString(buffer *bytes.Buffer, value string) error {
	if len(value) > maxCheckpointFieldBytes {
		return fmt.Errorf("campo de checkpoint supera %d bytes", maxCheckpointFieldBytes)
	}
	return writeCheckpointBytes(buffer, []byte(value))
}

func writeCheckpointBytes(buffer *bytes.Buffer, value []byte) error {
	if len(value) > maxCheckpointFieldBytes && len(value) != ed25519.SignatureSize {
		return fmt.Errorf("campo binario de checkpoint supera el límite permitido")
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buffer.Write(length[:])
	buffer.Write(value)
	return nil
}

func isCanonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16
}

func isCanonicalSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
