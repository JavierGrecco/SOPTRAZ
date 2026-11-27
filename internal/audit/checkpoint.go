package audit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	CheckpointSchemaVersion                     = "1.0.0"
	CheckpointSignatureAlgorithm                = "ed25519"
	CheckpointSignatureAlgorithmECDSAP256SHA256 = "ecdsa-p256-sha256"
	checkpointDomainSeparator                   = "SOPTRAZ-AUDIT-CHECKPOINT-V1\x00"
	checkpointDigestDomain                      = "SOPTRAZ-AUDIT-CHECKPOINT-DIGEST-V1\x00"
	maxCheckpointFieldBytes                     = 4096
)

// Checkpoint commits to one immutable head of an intervention audit chain.
// CreatedAt is informational and is not a trusted timestamp.
type Checkpoint struct {
	SchemaVersion          string `json:"schemaVersion"`
	CheckpointID           string `json:"checkpointId"`
	InterventionID         string `json:"interventionId"`
	EventSchemaVersion     string `json:"eventSchemaVersion"`
	HashAlgorithm          string `json:"hashAlgorithm"`
	SignatureAlgorithm     string `json:"signatureAlgorithm"`
	Sequence               uint64 `json:"sequence"`
	HeadHash               string `json:"headHash"`
	PreviousCheckpointHash string `json:"previousCheckpointHash,omitempty"`
	CreatedAt              string `json:"createdAt"`
	KeyID                  string `json:"keyId"`
}

// SignedCheckpoint contains a checkpoint and its Ed25519 signature.
// The private key is never included in this structure or serialized form.
type SignedCheckpoint struct {
	Checkpoint Checkpoint `json:"checkpoint"`
	Signature  string     `json:"signature"`
}

// CheckpointPublicKey contains one supported public-key representation.
// Exactly one key field must be populated and match SignatureAlgorithm.
type CheckpointPublicKey struct {
	SignatureAlgorithm string
	Ed25519            ed25519.PublicKey
	ECDSAP256          *ecdsa.PublicKey
}

// CheckpointKeyIdentity is the public identity selected by a signer.
type CheckpointKeyIdentity struct {
	KeyID              string
	SignatureAlgorithm string
}

// CheckpointKeyResolver resolves public material from an independently
// managed trust store. Bundled public keys are never trusted automatically.
type CheckpointKeyResolver interface {
	ResolvePublicKey(ctx context.Context, keyID string) (CheckpointPublicKey, error)
}

// CheckpointSigner keeps private-key operations outside the audit store.
// ECDSA implementations return canonical 64-byte P1363 r||s with low-S.
type CheckpointSigner interface {
	// Identity returns the public key corresponding to the private operation.
	// The returned key is used to verify the signature before it is published.
	Identity(ctx context.Context) (CheckpointPublicKey, error)
	// Sign returns 64-byte Ed25519 or IEEE P1363 r||s ECDSA P-256 low-S.
	Sign(ctx context.Context, message []byte) ([]byte, error)
}

func supportedCheckpointSignatureAlgorithm(value string) bool {
	return value == CheckpointSignatureAlgorithm || value == CheckpointSignatureAlgorithmECDSAP256SHA256
}

// CanonicalBytes returns the public key's canonical fingerprint material:
// raw 32-byte Ed25519 or uncompressed SEC1 P-256 (0x04 || X || Y).
func (publicKey CheckpointPublicKey) CanonicalBytes() ([]byte, error) {
	switch publicKey.SignatureAlgorithm {
	case CheckpointSignatureAlgorithm:
		if len(publicKey.Ed25519) != ed25519.PublicKeySize || publicKey.ECDSAP256 != nil {
			return nil, fmt.Errorf("clave pública Ed25519 inválida o ambigua")
		}
		return append([]byte(nil), publicKey.Ed25519...), nil
	case CheckpointSignatureAlgorithmECDSAP256SHA256:
		if publicKey.Ed25519 != nil || publicKey.ECDSAP256 == nil || publicKey.ECDSAP256.X == nil || publicKey.ECDSAP256.Y == nil {
			return nil, fmt.Errorf("clave pública ECDSA P-256 inválida o ambigua")
		}
		curve := elliptic.P256()
		if !sameCurve(publicKey.ECDSAP256.Curve, curve) || !curve.IsOnCurve(publicKey.ECDSAP256.X, publicKey.ECDSAP256.Y) {
			return nil, fmt.Errorf("la clave pública no pertenece a NIST P-256")
		}
		return elliptic.Marshal(curve, publicKey.ECDSAP256.X, publicKey.ECDSAP256.Y), nil
	default:
		return nil, fmt.Errorf("algoritmo de firma de checkpoint no compatible %q", publicKey.SignatureAlgorithm)
	}
}

// sameCurve compares full parameters instead of trusting an external display name.
func sameCurve(left, right elliptic.Curve) bool {
	if left == nil || right == nil {
		return false
	}
	leftParams := left.Params()
	rightParams := right.Params()
	if leftParams == nil || rightParams == nil {
		return false
	}
	// Curve implementations supplied by external hardware providers are not
	// trusted to return complete parameters. Reject incomplete values instead
	// of dereferencing nil big.Int fields during key validation.
	if leftParams.P == nil || rightParams.P == nil ||
		leftParams.N == nil || rightParams.N == nil ||
		leftParams.B == nil || rightParams.B == nil ||
		leftParams.Gx == nil || rightParams.Gx == nil ||
		leftParams.Gy == nil || rightParams.Gy == nil {
		return false
	}
	return leftParams.Name == rightParams.Name &&
		leftParams.BitSize == rightParams.BitSize &&
		leftParams.P.Cmp(rightParams.P) == 0 &&
		leftParams.N.Cmp(rightParams.N) == 0 &&
		leftParams.B.Cmp(rightParams.B) == 0 &&
		leftParams.Gx.Cmp(rightParams.Gx) == 0 &&
		leftParams.Gy.Cmp(rightParams.Gy) == 0
}

// CheckpointKeyIDFromPublicKey hashes the canonical public-key representation.
func CheckpointKeyIDFromPublicKey(publicKey CheckpointPublicKey) (string, error) {
	canonical, err := publicKey.CanonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// CheckpointKeyIdentityFromPublicKey derives the signer identity without
// exposing or exporting its private key.
func CheckpointKeyIdentityFromPublicKey(publicKey CheckpointPublicKey) (CheckpointKeyIdentity, error) {
	keyID, err := CheckpointKeyIDFromPublicKey(publicKey)
	if err != nil {
		return CheckpointKeyIdentity{}, err
	}
	return CheckpointKeyIdentity{KeyID: keyID, SignatureAlgorithm: publicKey.SignatureAlgorithm}, nil
}

func (identity CheckpointKeyIdentity) Validate() error {
	if !supportedCheckpointSignatureAlgorithm(identity.SignatureAlgorithm) {
		return fmt.Errorf("algoritmo de firma no compatible %q", identity.SignatureAlgorithm)
	}
	if !isCanonicalSHA256(identity.KeyID) {
		return fmt.Errorf("keyId debe ser SHA-256 hexadecimal en minúsculas del material público canónico")
	}
	return nil
}

// NewCheckpointWithIdentity creates a checkpoint using an externally selected
// signer identity. It does not create, load or store private keys.
func NewCheckpointWithIdentity(interventionID string, sequence uint64, headHash, previousCheckpointHash string, identity CheckpointKeyIdentity, createdAt time.Time) (Checkpoint, error) {
	if createdAt.IsZero() {
		return Checkpoint{}, fmt.Errorf("la fecha del checkpoint no puede estar vacía")
	}
	if err := identity.Validate(); err != nil {
		return Checkpoint{}, err
	}
	checkpointID, err := NewEventID()
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{
		SchemaVersion:          CheckpointSchemaVersion,
		CheckpointID:           checkpointID,
		InterventionID:         interventionID,
		EventSchemaVersion:     SchemaVersion,
		HashAlgorithm:          HashAlgorithm,
		SignatureAlgorithm:     identity.SignatureAlgorithm,
		Sequence:               sequence,
		HeadHash:               headHash,
		PreviousCheckpointHash: previousCheckpointHash,
		CreatedAt:              createdAt.UTC().Format(time.RFC3339Nano),
		KeyID:                  identity.KeyID,
	}
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// NewCheckpoint is the Ed25519 convenience constructor retained for callers
// that explicitly choose an Ed25519 signer.
func NewCheckpoint(interventionID string, sequence uint64, headHash, previousCheckpointHash string, publicKey ed25519.PublicKey, createdAt time.Time) (Checkpoint, error) {
	identity, err := CheckpointKeyIdentityFromPublicKey(CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithm,
		Ed25519:            publicKey,
	})
	if err != nil {
		return Checkpoint{}, err
	}
	return NewCheckpointWithIdentity(interventionID, sequence, headHash, previousCheckpointHash, identity, createdAt)
}

// CheckpointKeyID is the Ed25519-specific compatibility helper.
func CheckpointKeyID(publicKey ed25519.PublicKey) (string, error) {
	return CheckpointKeyIDFromPublicKey(CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithm,
		Ed25519:            publicKey,
	})
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
	if !supportedCheckpointSignatureAlgorithm(checkpoint.SignatureAlgorithm) {
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

// ed25519CheckpointSigner adapts a software key for tests and explicit
// development use. Production adapters must keep keys in protected storage.
type ed25519CheckpointSigner struct {
	privateKey ed25519.PrivateKey
}

func (signer ed25519CheckpointSigner) Identity(ctx context.Context) (CheckpointPublicKey, error) {
	if ctx == nil {
		return CheckpointPublicKey{}, fmt.Errorf("el contexto del firmante no puede ser nil")
	}
	if err := ctx.Err(); err != nil {
		return CheckpointPublicKey{}, err
	}
	if len(signer.privateKey) != ed25519.PrivateKeySize {
		return CheckpointPublicKey{}, fmt.Errorf("clave privada Ed25519 inválida: longitud %d", len(signer.privateKey))
	}
	publicKey := signer.privateKey.Public().(ed25519.PublicKey)
	return CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithm,
		Ed25519:            publicKey,
	}, nil
}

func (signer ed25519CheckpointSigner) Sign(ctx context.Context, message []byte) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("el contexto del firmante no puede ser nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(signer.privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("clave privada Ed25519 inválida: longitud %d", len(signer.privateKey))
	}
	return ed25519.Sign(signer.privateKey, message), nil
}

// SignCheckpoint retains the Ed25519 convenience API. New OS-backed signers
// should implement CheckpointSigner and call SignCheckpointWithSigner.
func SignCheckpoint(checkpoint Checkpoint, privateKey ed25519.PrivateKey) (SignedCheckpoint, error) {
	return SignCheckpointWithSigner(context.Background(), checkpoint, ed25519CheckpointSigner{privateKey: privateKey})
}

// SignCheckpointWithSigner signs the canonical preimage without exposing the
// private key to the checkpoint domain or SQLite adapter.
func SignCheckpointWithSigner(ctx context.Context, checkpoint Checkpoint, signer CheckpointSigner) (SignedCheckpoint, error) {
	if ctx == nil {
		return SignedCheckpoint{}, fmt.Errorf("el contexto de firma no puede ser nil")
	}
	if signer == nil {
		return SignedCheckpoint{}, fmt.Errorf("el firmante de checkpoints no puede ser nil")
	}
	publicKey, err := signer.Identity(ctx)
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("obtener clave pública del firmante: %w", err)
	}
	identity, err := CheckpointKeyIdentityFromPublicKey(publicKey)
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("validar clave pública del firmante: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return SignedCheckpoint{}, err
	}
	if checkpoint.SignatureAlgorithm == "" {
		checkpoint.SignatureAlgorithm = identity.SignatureAlgorithm
	} else if checkpoint.SignatureAlgorithm != identity.SignatureAlgorithm {
		return SignedCheckpoint{}, fmt.Errorf("el algoritmo del checkpoint no coincide con el firmante")
	}
	if checkpoint.KeyID == "" {
		checkpoint.KeyID = identity.KeyID
	} else if checkpoint.KeyID != identity.KeyID {
		return SignedCheckpoint{}, fmt.Errorf("keyId del checkpoint no coincide con el firmante")
	}
	message, err := CheckpointSigningBytes(checkpoint)
	if err != nil {
		return SignedCheckpoint{}, err
	}
	signature, err := signer.Sign(ctx, message)
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("firmar checkpoint: %w", err)
	}
	if err := validateCheckpointSignature(checkpoint.SignatureAlgorithm, signature); err != nil {
		return SignedCheckpoint{}, err
	}
	signed := SignedCheckpoint{Checkpoint: checkpoint, Signature: base64.StdEncoding.EncodeToString(signature)}
	if err := VerifySignedCheckpointWithPublicKey(signed, publicKey); err != nil {
		return SignedCheckpoint{}, fmt.Errorf("la firma no corresponde a la clave pública declarada por el proveedor: %w", err)
	}
	return signed, nil
}

// VerifySignedCheckpoint is the Ed25519-specific compatibility verifier.
func VerifySignedCheckpoint(signed SignedCheckpoint, publicKey ed25519.PublicKey) error {
	return VerifySignedCheckpointWithPublicKey(signed, CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithm,
		Ed25519:            publicKey,
	})
}

// VerifySignedCheckpointWithPublicKey validates the declared algorithm,
// canonical key fingerprint, signature encoding and cryptographic signature.
func VerifySignedCheckpointWithPublicKey(signed SignedCheckpoint, publicKey CheckpointPublicKey) error {
	if err := signed.Checkpoint.Validate(); err != nil {
		return err
	}
	if publicKey.SignatureAlgorithm != signed.Checkpoint.SignatureAlgorithm {
		return fmt.Errorf("el algoritmo de la clave pública no coincide con el checkpoint")
	}
	keyID, err := CheckpointKeyIDFromPublicKey(publicKey)
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
	signature, err := decodeCheckpointSignature(signed.Signature, signed.Checkpoint.SignatureAlgorithm)
	if err != nil {
		return err
	}
	switch signed.Checkpoint.SignatureAlgorithm {
	case CheckpointSignatureAlgorithm:
		if !ed25519.Verify(publicKey.Ed25519, message, signature) {
			return fmt.Errorf("firma Ed25519 del checkpoint inválida")
		}
	case CheckpointSignatureAlgorithmECDSAP256SHA256:
		digest := sha256.Sum256(message)
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		ecdsaKey := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).Set(publicKey.ECDSAP256.X),
			Y:     new(big.Int).Set(publicKey.ECDSAP256.Y),
		}
		if !ecdsa.Verify(ecdsaKey, digest[:], r, s) {
			return fmt.Errorf("firma ECDSA P-256 del checkpoint inválida")
		}
	default:
		return fmt.Errorf("algoritmo de firma de checkpoint no compatible %q", signed.Checkpoint.SignatureAlgorithm)
	}
	return nil
}

// VerifyCheckpointWithTrustStore resolves the key from an independently
// managed trust store before checking the signature.
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
	return VerifySignedCheckpointWithPublicKey(signed, publicKey)
}

// CheckpointDigest returns a digest of the canonical checkpoint statement,
// not the signature bytes. It verifies the signature against an independent
// resolver first, so an untrusted signature can never be anchored.
func CheckpointDigest(ctx context.Context, signed SignedCheckpoint, resolver CheckpointKeyResolver) (string, error) {
	if err := VerifyCheckpointWithTrustStore(ctx, signed, resolver); err != nil {
		return "", fmt.Errorf("verificar checkpoint antes de calcular digest: %w", err)
	}
	message, err := CheckpointSigningBytes(signed.Checkpoint)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	buffer.WriteString(checkpointDigestDomain)
	if err := writeCheckpointBytes(&buffer, message); err != nil {
		return "", err
	}
	digest := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func decodeCheckpointSignature(value, algorithm string) ([]byte, error) {
	signature, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("signature base64 inválida: %w", err)
	}
	if base64.StdEncoding.EncodeToString(signature) != value {
		return nil, fmt.Errorf("signature base64 no canónica")
	}
	if err := validateCheckpointSignature(algorithm, signature); err != nil {
		return nil, err
	}
	return signature, nil
}

func validateCheckpointSignature(algorithm string, signature []byte) error {
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("signature de checkpoint inválida: se requieren 64 bytes")
	}
	if algorithm == CheckpointSignatureAlgorithmECDSAP256SHA256 {
		n := elliptic.P256().Params().N
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		halfOrder := new(big.Int).Rsh(new(big.Int).Set(n), 1)
		if r.Sign() <= 0 || r.Cmp(n) >= 0 || s.Sign() <= 0 || s.Cmp(halfOrder) > 0 {
			return fmt.Errorf("firma ECDSA P-256 no canónica: r/s fuera de rango o high-S")
		}
		return nil
	}
	if algorithm != CheckpointSignatureAlgorithm {
		return fmt.Errorf("algoritmo de firma de checkpoint no compatible %q", algorithm)
	}
	return nil
}

func writeCheckpointString(buffer *bytes.Buffer, value string) error {
	if len(value) > maxCheckpointFieldBytes {
		return fmt.Errorf("campo de checkpoint supera %d bytes", maxCheckpointFieldBytes)
	}
	return writeCheckpointBytes(buffer, []byte(value))
}

func writeCheckpointBytes(buffer *bytes.Buffer, value []byte) error {
	if len(value) > maxCheckpointFieldBytes {
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
