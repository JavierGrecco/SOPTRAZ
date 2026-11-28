package catalog

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

type testSigningKey struct {
	private ed25519.PrivateKey
	signer  signature.Signer
}

func newTestSigningKey(t *testing.T) testSigningKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}
	signer, err := signature.LoadSigner(private, crypto.SHA256)
	if err != nil {
		t.Fatalf("load TUF signer: %v", err)
	}
	return testSigningKey{private: private, signer: signer}
}

func makeSignedRoot(t *testing.T, version int64, signers ...testSigningKey) []byte {
	t.Helper()
	root := metadata.Root(time.Now().UTC().AddDate(1, 0, 0))
	root.Signed.Version = version
	for _, key := range signers {
		publicKey, ok := key.private.Public().(ed25519.PublicKey)
		if !ok {
			t.Fatal("test signer returned a non-Ed25519 public key")
		}
		tufKey, err := metadata.KeyFromPublicKey(publicKey)
		if err != nil {
			t.Fatalf("convert public key to TUF key: %v", err)
		}
		if err := root.Signed.AddKey(tufKey, metadata.ROOT); err != nil {
			t.Fatalf("add root key: %v", err)
		}
	}
	for _, key := range signers {
		if _, err := root.Sign(key.signer); err != nil {
			t.Fatalf("sign root metadata: %v", err)
		}
	}
	raw, err := root.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal root metadata: %v", err)
	}
	return raw
}

func rootPin(root []byte) string {
	digest := sha256.Sum256(root)
	return hex.EncodeToString(digest[:])
}

func TestNewPinnedTrustAnchorRequiresExactIndependentPin(t *testing.T) {
	key := newTestSigningKey(t)
	root := makeSignedRoot(t, 1, key)

	anchor, err := NewPinnedTrustAnchor(root, rootPin(root))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	state, err := anchor.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Version != 1 || state.SHA256 != rootPin(root) {
		t.Fatalf("unexpected root state: %#v", state)
	}

	if _, err := NewPinnedTrustAnchor(root, rootPin([]byte("different root"))); !errors.Is(err, ErrInvalidTrustAnchor) {
		t.Fatalf("wrong pin error = %v, want ErrInvalidTrustAnchor", err)
	}
}

func TestNewPinnedTrustAnchorRejectsSelfSignatureTampering(t *testing.T) {
	key := newTestSigningKey(t)
	rootBytes := makeSignedRoot(t, 1, key)
	root, err := metadata.Root().FromBytes(rootBytes)
	if err != nil {
		t.Fatalf("parse test root: %v", err)
	}
	root.Signatures[0].Signature[0] ^= 0x80
	tampered, err := root.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal tampered root: %v", err)
	}

	// The pin matches the tampered bytes. Rejection must therefore come from
	// go-tuf's cryptographic root verification, not from the pin comparison.
	if _, err := NewPinnedTrustAnchor(tampered, rootPin(tampered)); !errors.Is(err, ErrInvalidTrustAnchor) {
		t.Fatalf("tampered root error = %v, want ErrInvalidTrustAnchor", err)
	}
}

func TestTrustAnchorRequiresConsecutiveRootRotation(t *testing.T) {
	oldKey := newTestSigningKey(t)
	newKey := newTestSigningKey(t)
	rootV1 := makeSignedRoot(t, 1, oldKey)
	anchor, err := NewPinnedTrustAnchor(rootV1, rootPin(rootV1))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}

	// The old root authorizes the transition and the new root signs itself.
	rootV2 := makeSignedRoot(t, 2, oldKey, newKey)
	state, err := anchor.VerifyAndRotateRoot(rootV2)
	if err != nil {
		t.Fatalf("VerifyAndRotateRoot(v2) error = %v", err)
	}
	if state.Version != 2 || state.SHA256 != rootPin(rootV2) {
		t.Fatalf("unexpected rotated state: %#v", state)
	}

	// A correctly signed but non-consecutive root version is a rollback/gap
	// policy violation. The previously trusted state must remain unchanged.
	rootV4 := makeSignedRoot(t, 4, oldKey, newKey)
	if _, err := anchor.VerifyAndRotateRoot(rootV4); !errors.Is(err, ErrInvalidRootUpdate) {
		t.Fatalf("VerifyAndRotateRoot(v4) error = %v, want ErrInvalidRootUpdate", err)
	}
	afterFailure, err := anchor.State()
	if err != nil {
		t.Fatalf("State() after rejected update error = %v", err)
	}
	if afterFailure.Version != 2 || afterFailure.SHA256 != rootPin(rootV2) {
		t.Fatalf("trust state changed after rejected root: %#v", afterFailure)
	}
}

func TestTrustAnchorRejectsRootNotSignedByPreviouslyTrustedKey(t *testing.T) {
	oldKey := newTestSigningKey(t)
	untrustedKey := newTestSigningKey(t)
	rootV1 := makeSignedRoot(t, 1, oldKey)
	anchor, err := NewPinnedTrustAnchor(rootV1, rootPin(rootV1))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}

	rootV2 := makeSignedRoot(t, 2, untrustedKey)
	if _, err := anchor.VerifyAndRotateRoot(rootV2); !errors.Is(err, ErrInvalidRootUpdate) {
		t.Fatalf("VerifyAndRotateRoot(untrusted) error = %v, want ErrInvalidRootUpdate", err)
	}
	state, err := anchor.State()
	if err != nil {
		t.Fatalf("State() after rejected update error = %v", err)
	}
	if state.Version != 1 || state.SHA256 != rootPin(rootV1) {
		t.Fatalf("trust state changed after rejected update: %#v", state)
	}
}


func TestTrustAnchorRetainsImmutableBootstrapRootAfterRotation(t *testing.T) {
	oldKey := newTestSigningKey(t)
	newKey := newTestSigningKey(t)
	rootV1 := makeSignedRoot(t, 1, oldKey)
	rootV2 := makeSignedRoot(t, 2, oldKey, newKey)
	anchor, err := NewPinnedTrustAnchor(rootV1, rootPin(rootV1))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	if _, err := anchor.VerifyAndRotateRoot(rootV2); err != nil {
		t.Fatalf("VerifyAndRotateRoot() error = %v", err)
	}

	initialJSON, initialState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("initialRootSnapshot() error = %v", err)
	}
	currentState, err := anchor.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if string(initialJSON) != string(rootV1) ||
		initialState.Version != 1 ||
		initialState.SHA256 != rootPin(rootV1) {
		t.Fatalf("bootstrap snapshot changed after rotation: state=%#v", initialState)
	}
	if currentState.Version != 2 || currentState.SHA256 != rootPin(rootV2) {
		t.Fatalf("current trust state = %#v, want rotated root v2", currentState)
	}
}
