package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func checkpointTestPrivateKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("SOPTRAZ checkpoint test key - never use in production"))
	return ed25519.NewKeyFromSeed(seed[:])
}

func checkpointTestValue(t *testing.T) (Checkpoint, ed25519.PrivateKey) {
	t.Helper()
	privateKey := checkpointTestPrivateKey()
	publicKey := privateKey.Public().(ed25519.PublicKey)
	checkpoint, err := NewCheckpoint(
		"2aef4e6d-9d50-4cb6-9c96-22a26abf1587",
		12,
		strings.Repeat("a", 64),
		"",
		publicKey,
		time.Date(2026, 11, 27, 20, 45, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint, privateKey
}

func TestCheckpointKeyIDUsesFullSHA256Fingerprint(t *testing.T) {
	privateKey := checkpointTestPrivateKey()
	publicKey := privateKey.Public().(ed25519.PublicKey)

	keyID, err := CheckpointKeyID(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(keyID)
	if err != nil {
		t.Fatalf("keyId is not hexadecimal: %v", err)
	}
	if len(decoded) != sha256.Size {
		t.Fatalf("keyId length = %d bytes, want %d", len(decoded), sha256.Size)
	}
}

func TestCheckpointSignAndVerify(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	checkpoint.KeyID = ""
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatalf("SignCheckpoint() error = %v", err)
	}
	if err := VerifySignedCheckpoint(signed, privateKey.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("VerifySignedCheckpoint() error = %v", err)
	}
	if signed.Checkpoint.KeyID == "" {
		t.Fatal("SignCheckpoint() did not derive keyId")
	}
}

func TestCheckpointRejectsTamperedFields(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}

	signed.Checkpoint.Sequence++
	if err := VerifySignedCheckpoint(signed, privateKey.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("tampered sequence must invalidate signature")
	}
}

func TestCheckpointRejectsWrongAndMalformedKeys(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}

	otherSeed := sha256.Sum256([]byte("a different checkpoint test key"))
	otherPrivate := ed25519.NewKeyFromSeed(otherSeed[:])
	if err := VerifySignedCheckpoint(signed, otherPrivate.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("wrong public key must be rejected")
	}
	if err := VerifySignedCheckpoint(signed, ed25519.PublicKey{1, 2, 3}); err == nil {
		t.Fatal("malformed public key must be rejected")
	}
	if _, err := SignCheckpoint(checkpoint, ed25519.PrivateKey{1, 2, 3}); err == nil {
		t.Fatal("malformed private key must be rejected")
	}
}

func TestCheckpointRejectsInvalidSignatureEncoding(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}

	signed.Signature = "not-base64"
	if err := VerifySignedCheckpoint(signed, privateKey.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("malformed signature must be rejected")
	}
}

func TestCheckpointDigestIsVerifiedStatementDigestAndChainSensitive(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	resolver := checkpointTestResolver{
		keyID: signed.Checkpoint.KeyID,
		key: CheckpointPublicKey{SignatureAlgorithm: CheckpointSignatureAlgorithm, Ed25519: publicKey},
	}
	digest1, err := CheckpointDigest(context.Background(), signed, resolver)
	if err != nil {
		t.Fatal(err)
	}
	digest2, err := CheckpointDigest(context.Background(), signed, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if digest1 != digest2 {
		t.Fatalf("checkpoint digest differs: %s != %s", digest1, digest2)
	}

	next := checkpoint
	next.Sequence++
	next.PreviousCheckpointHash = digest1
	signedNext, err := SignCheckpoint(next, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	digestNext, err := CheckpointDigest(context.Background(), signedNext, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if digestNext == digest1 {
		t.Fatal("next checkpoint digest must differ from its predecessor")
	}
}

func TestECDSAP256CheckpointSignVerifyAndCanonicalDigest(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithmECDSAP256SHA256,
		ECDSAP256:           &privateKey.PublicKey,
	}
	identity, err := CheckpointKeyIdentityFromPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := NewCheckpointWithIdentity(
		"2aef4e6d-9d50-4cb6-9c96-22a26abf1587",
		12,
		strings.Repeat("a", 64),
		"",
		identity,
		time.Date(2026, 11, 27, 20, 45, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	signer := checkpointTestECDSASigner{privateKey: privateKey}
	first, err := SignCheckpointWithSigner(context.Background(), checkpoint, signer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignCheckpointWithSigner(context.Background(), checkpoint, signer)
	if err != nil {
		t.Fatal(err)
	}
	resolver := checkpointTestResolver{keyID: identity.KeyID, key: publicKey}
	if err := VerifyCheckpointWithTrustStore(context.Background(), first, resolver); err != nil {
		t.Fatalf("first signature verification: %v", err)
	}
	if err := VerifyCheckpointWithTrustStore(context.Background(), second, resolver); err != nil {
		t.Fatalf("second signature verification: %v", err)
	}
	digest1, err := CheckpointDigest(context.Background(), first, resolver)
	if err != nil {
		t.Fatal(err)
	}
	digest2, err := CheckpointDigest(context.Background(), second, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if digest1 != digest2 {
		t.Fatalf("same canonical ECDSA statement produced different digests: %s != %s", digest1, digest2)
	}
}

func TestECDSAP256RejectsHighSSignature(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithmECDSAP256SHA256,
		ECDSAP256:           &privateKey.PublicKey,
	}
	identity, err := CheckpointKeyIdentityFromPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := NewCheckpointWithIdentity("2aef4e6d-9d50-4cb6-9c96-22a26abf1587", 1, strings.Repeat("c", 64), "", identity, time.Date(2026, 11, 27, 20, 45, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignCheckpointWithSigner(context.Background(), checkpoint, checkpointTestECDSASigner{privateKey: privateKey})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	order := elliptic.P256().Params().N
	s := new(big.Int).SetBytes(signature[32:])
	s.Sub(order, s)
	s.FillBytes(signature[32:])
	signed.Signature = base64.StdEncoding.EncodeToString(signature)
	if err := VerifySignedCheckpointWithPublicKey(signed, publicKey); err == nil || !strings.Contains(err.Error(), "high-S") {
		t.Fatalf("high-S signature error = %v, want canonicality rejection", err)
	}
}

func TestCheckpointDigestRejectsInvalidSignatureBeforeReturningAnchor(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	resolver := checkpointTestResolver{keyID: signed.Checkpoint.KeyID, key: CheckpointPublicKey{SignatureAlgorithm: CheckpointSignatureAlgorithm, Ed25519: publicKey}}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 0x01
	signed.Signature = base64.StdEncoding.EncodeToString(signature)
	if _, err := CheckpointDigest(context.Background(), signed, resolver); err == nil {
		t.Fatal("digest must not be returned for an invalid signature")
	}
}

type checkpointTestECDSASigner struct {
	privateKey *ecdsa.PrivateKey
}

func (signer checkpointTestECDSASigner) Identity(_ context.Context) (CheckpointKeyIdentity, error) {
	return CheckpointKeyIdentityFromPublicKey(CheckpointPublicKey{
		SignatureAlgorithm: CheckpointSignatureAlgorithmECDSAP256SHA256,
		ECDSAP256:           &signer.privateKey.PublicKey,
	})
}

func (signer checkpointTestECDSASigner) Sign(_ context.Context, message []byte) ([]byte, error) {
	digest := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, signer.privateKey, digest[:])
	if err != nil {
		return nil, err
	}
	order := elliptic.P256().Params().N
	halfOrder := new(big.Int).Rsh(new(big.Int).Set(order), 1)
	if s.Cmp(halfOrder) > 0 {
		s.Sub(order, s)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signature, nil
}

func TestCheckpointSigningBytesAreStableAndVersioned(t *testing.T) {
	checkpoint, _ := checkpointTestValue(t)
	first, err := CheckpointSigningBytes(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CheckpointSigningBytes(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("checkpoint signing preimage is not deterministic")
	}
	if !bytes.HasPrefix(first, []byte(checkpointDomainSeparator)) {
		t.Fatal("checkpoint signing preimage is missing its domain separator")
	}
	checkpoint.Sequence++
	changed, err := CheckpointSigningBytes(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, changed) {
		t.Fatal("changing a signed field did not change signing preimage")
	}
}

func TestCheckpointValidateRejectsNonCanonicalTimestampAndHashes(t *testing.T) {
	checkpoint, _ := checkpointTestValue(t)
	cases := []struct {
		name   string
		change func(*Checkpoint)
	}{
		{"uppercase head hash", func(value *Checkpoint) { value.HeadHash = strings.Repeat("A", 64) }},
		{"short previous hash", func(value *Checkpoint) { value.PreviousCheckpointHash = "abcd" }},
		{"non-UTC timestamp", func(value *Checkpoint) { value.CreatedAt = "2026-11-27T20:45:00+01:00" }},
		{"non-canonical timestamp", func(value *Checkpoint) { value.CreatedAt = "2026-11-27T20:45:00.000Z" }},
		{"unsupported hash", func(value *Checkpoint) { value.HashAlgorithm = "sha512" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := checkpoint
			tc.change(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("invalid checkpoint must be rejected")
			}
		})
	}
}

type checkpointTestResolver struct {
	keyID string
	key   CheckpointPublicKey
	err   error
}

func (resolver checkpointTestResolver) ResolvePublicKey(_ context.Context, keyID string) (CheckpointPublicKey, error) {
	if resolver.err != nil {
		return CheckpointPublicKey{}, resolver.err
	}
	if keyID != resolver.keyID {
		return CheckpointPublicKey{}, errors.New("key ID desconocido")
	}
	return resolver.key, nil
}

func TestVerifyCheckpointWithTrustStore(t *testing.T) {
	checkpoint, privateKey := checkpointTestValue(t)
	signed, err := SignCheckpoint(checkpoint, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	resolver := checkpointTestResolver{keyID: signed.Checkpoint.KeyID, key: publicKey}

	if err := VerifyCheckpointWithTrustStore(context.Background(), signed, resolver); err != nil {
		t.Fatalf("VerifyCheckpointWithTrustStore() error = %v", err)
	}
	if err := VerifyCheckpointWithTrustStore(context.Background(), signed, checkpointTestResolver{err: errors.New("unknown key")}); err == nil {
		t.Fatal("unknown key must fail closed")
	}
}
