package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

type tufRepositoryFixture struct {
	rootJSON      []byte
	timestampJSON []byte
	snapshotJSON  []byte
	targetsJSON   []byte
	targetPath    string
	targetBytes   []byte
	targetSHA256  string
}

func makeTUFRepositoryFixture(t *testing.T, timestampExpiry time.Time) tufRepositoryFixture {
	t.Helper()
	expiry := time.Now().UTC().AddDate(0, 0, 7)
	rootKey := newTestSigningKey(t)
	timestampKey := newTestSigningKey(t)
	snapshotKey := newTestSigningKey(t)
	targetsKey := newTestSigningKey(t)

	root := metadata.Root(expiry)
	for _, entry := range []struct {
		key  testSigningKey
		role string
	}{
		{rootKey, metadata.ROOT},
		{timestampKey, metadata.TIMESTAMP},
		{snapshotKey, metadata.SNAPSHOT},
		{targetsKey, metadata.TARGETS},
	} {
		publicKey, ok := entry.key.private.Public().(ed25519.PublicKey)
		if !ok {
			t.Fatal("test signer returned a non-Ed25519 public key")
		}
		tufKey, err := metadata.KeyFromPublicKey(publicKey)
		if err != nil {
			t.Fatalf("convert TUF key: %v", err)
		}
		if err := root.Signed.AddKey(tufKey, entry.role); err != nil {
			t.Fatalf("delegate %s role: %v", entry.role, err)
		}
	}
	if _, err := root.Sign(rootKey.signer); err != nil {
		t.Fatalf("sign root: %v", err)
	}
	rootJSON, err := root.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal root: %v", err)
	}

	targetPath := "osquery/5.24.1/linux-amd64/osqueryi"
	targetBytes := []byte("trusted osquery fixture payload")
	targetDigest := sha256.Sum256(targetBytes)
	targets := metadata.Targets(expiry)
	targets.Signed.Targets[targetPath] = &metadata.TargetFiles{
		Length: int64(len(targetBytes)),
		Hashes: metadata.Hashes{"sha256": metadata.HexBytes(targetDigest[:])},
	}
	if _, err := targets.Sign(targetsKey.signer); err != nil {
		t.Fatalf("sign targets: %v", err)
	}
	targetsJSON, err := targets.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal targets: %v", err)
	}

	targetsMeta := metadata.MetaFile(1)
	targetsMeta.Length = int64(len(targetsJSON))
	targetsHash := sha256.Sum256(targetsJSON)
	targetsMeta.Hashes = metadata.Hashes{"sha256": metadata.HexBytes(targetsHash[:])}
	snapshot := metadata.Snapshot(expiry)
	snapshot.Signed.Meta["targets.json"] = targetsMeta
	if _, err := snapshot.Sign(snapshotKey.signer); err != nil {
		t.Fatalf("sign snapshot: %v", err)
	}
	snapshotJSON, err := snapshot.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	snapshotMeta := metadata.MetaFile(1)
	snapshotMeta.Length = int64(len(snapshotJSON))
	snapshotHash := sha256.Sum256(snapshotJSON)
	snapshotMeta.Hashes = metadata.Hashes{"sha256": metadata.HexBytes(snapshotHash[:])}
	timestamp := metadata.Timestamp(expiry)
	timestamp.Signed.Meta["snapshot.json"] = snapshotMeta
	timestamp.Signed.Expires = timestampExpiry
	if _, err := timestamp.Sign(timestampKey.signer); err != nil {
		t.Fatalf("sign timestamp: %v", err)
	}
	timestampJSON, err := timestamp.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal timestamp: %v", err)
	}

	return tufRepositoryFixture{
		rootJSON:      rootJSON,
		timestampJSON: timestampJSON,
		snapshotJSON:  snapshotJSON,
		targetsJSON:   targetsJSON,
		targetPath:    targetPath,
		targetBytes:   targetBytes,
		targetSHA256:  hex.EncodeToString(targetDigest[:]),
	}
}

func newFixtureSession(t *testing.T, fixture tufRepositoryFixture) *MetadataSession {
	t.Helper()
	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	session, err := anchor.NewMetadataSession()
	if err != nil {
		t.Fatalf("NewMetadataSession() error = %v", err)
	}
	return session
}

func TestMetadataSessionAuthenticatesCompleteRoleChainAndTarget(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)

	if err := session.UpdateTimestamp(fixture.timestampJSON); err != nil {
		t.Fatalf("UpdateTimestamp() error = %v", err)
	}
	if err := session.UpdateSnapshot(fixture.snapshotJSON); err != nil {
		t.Fatalf("UpdateSnapshot() error = %v", err)
	}
	if err := session.UpdateTargets(fixture.targetsJSON); err != nil {
		t.Fatalf("UpdateTargets() error = %v", err)
	}
	target, err := session.Target(fixture.targetPath)
	if err != nil {
		t.Fatalf("Target() error = %v", err)
	}
	if target.Length != int64(len(fixture.targetBytes)) || target.SHA256 != fixture.targetSHA256 {
		t.Fatalf("unexpected authenticated target: %#v", target)
	}
}

func TestMetadataSessionRejectsSnapshotHashMismatchAndPoisonsSession(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	if err := session.UpdateTimestamp(fixture.timestampJSON); err != nil {
		t.Fatalf("UpdateTimestamp() error = %v", err)
	}

	tampered := append([]byte(nil), fixture.snapshotJSON...)
	tampered[len(tampered)-2] ^= 1
	if err := session.UpdateSnapshot(tampered); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("UpdateSnapshot() error = %v, want ErrInvalidSnapshot", err)
	}
	if err := session.UpdateTargets(fixture.targetsJSON); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("UpdateTargets() after failure = %v, want poisoned session error", err)
	}
}

func TestMetadataSessionRejectsTargetsHashMismatch(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	if err := session.UpdateTimestamp(fixture.timestampJSON); err != nil {
		t.Fatalf("UpdateTimestamp() error = %v", err)
	}
	if err := session.UpdateSnapshot(fixture.snapshotJSON); err != nil {
		t.Fatalf("UpdateSnapshot() error = %v", err)
	}

	tampered := append([]byte(nil), fixture.targetsJSON...)
	tampered[len(tampered)-2] ^= 1
	if err := session.UpdateTargets(tampered); !errors.Is(err, ErrInvalidTargets) {
		t.Fatalf("UpdateTargets() error = %v, want ErrInvalidTargets", err)
	}
}

func TestMetadataSessionRejectsExpiredTimestamp(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().Add(-time.Hour))
	session := newFixtureSession(t, fixture)
	if err := session.UpdateTimestamp(fixture.timestampJSON); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("UpdateTimestamp() error = %v, want ErrInvalidTimestamp", err)
	}
	if _, err := session.Target(fixture.targetPath); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("Target() after expiry failure = %v, want ErrInvalidTimestamp", err)
	}
}

func TestMetadataSessionRequiresOrderedStagesAndEnforcesMetadataLimits(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	if err := session.UpdateSnapshot(fixture.snapshotJSON); !errors.Is(err, ErrMetadataSequence) {
		t.Fatalf("UpdateSnapshot() before timestamp = %v, want ErrMetadataSequence", err)
	}
	if err := session.UpdateTimestamp(fixture.timestampJSON); !errors.Is(err, ErrMetadataSequence) {
		t.Fatalf("UpdateTimestamp() after sequence failure = %v, want poisoned session error", err)
	}

	session = newFixtureSession(t, fixture)
	oversized := make([]byte, MaxTimestampMetadataBytes+1)
	if err := session.UpdateTimestamp(oversized); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("UpdateTimestamp() oversized = %v, want ErrInvalidTimestamp", err)
	}
}
