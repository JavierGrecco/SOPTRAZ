package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
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
	descriptor    artifact.Descriptor
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
	descriptor := artifact.Descriptor{
		SchemaVersion: artifact.ArtifactDescriptorSchemaVersion,
		ID:            "soptraz.osquery",
		Version:       "5.24.1",
		OS:            "linux",
		Architecture:  "amd64",
		RelativePath:  targetPath,
		SHA256:        hex.EncodeToString(targetDigest[:]),
		SizeBytes:     int64(len(targetBytes)),
		License:       "Apache-2.0 OR GPL-2.0-only",
		SourceURI:     "https://github.com/osquery/osquery/releases/tag/5.24.1",
	}
	customJSON, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal artifact descriptor: %v", err)
	}
	custom := json.RawMessage(customJSON)
	targets := metadata.Targets(expiry)
	targets.Signed.Targets[targetPath] = &metadata.TargetFiles{
		Length: int64(len(targetBytes)),
		Hashes: metadata.Hashes{"sha256": metadata.HexBytes(targetDigest[:])},
		Custom: &custom,
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
		descriptor:    descriptor,
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

func TestMetadataSessionRejectsNonPortableTargetPaths(t *testing.T) {
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

	invalidPaths := []string{
		`osquery\binary`,
		`osquery\\binary`,
		`C:\Windows\osquery.exe`,
		`C:/Windows/osquery.exe`,
		"../escape",
		"/absolute/path",
		"osquery//binary",
		"osquery/./binary",
		"osquery/",
		"NUL.exe",
		"COM1",
		"folder.",
		"folder ",
		"bad|name",
	}
	for _, candidate := range invalidPaths {
		t.Run(candidate, func(t *testing.T) {
			if _, err := session.Target(candidate); !errors.Is(err, ErrInvalidTarget) {
				t.Fatalf("Target(%q) error = %v, want ErrInvalidTarget", candidate, err)
			}
		})
	}
}

func applyValidMetadataChain(t *testing.T, session *MetadataSession, fixture tufRepositoryFixture) {
	t.Helper()
	if err := session.UpdateTimestamp(fixture.timestampJSON); err != nil {
		t.Fatalf("UpdateTimestamp() error = %v", err)
	}
	if err := session.UpdateSnapshot(fixture.snapshotJSON); err != nil {
		t.Fatalf("UpdateSnapshot() error = %v", err)
	}
	if err := session.UpdateTargets(fixture.targetsJSON); err != nil {
		t.Fatalf("UpdateTargets() error = %v", err)
	}
}

func TestMetadataSessionVerifiesSignedArtifactDescriptor(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	applyValidMetadataChain(t, session, fixture)

	target, err := session.VerifyDescriptor(fixture.descriptor)
	if err != nil {
		t.Fatalf("VerifyDescriptor() error = %v", err)
	}
	if target.Path != fixture.targetPath || target.Length != int64(len(fixture.targetBytes)) || target.SHA256 != fixture.targetSHA256 {
		t.Fatalf("unexpected verified target: %#v", target)
	}
}

func TestMetadataSessionRejectsArtifactDescriptorFieldMismatches(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	tests := []struct {
		name   string
		mutate func(*artifact.Descriptor)
	}{
		{name: "artifact ID", mutate: func(d *artifact.Descriptor) { d.ID = "soptraz.other" }},
		{name: "version", mutate: func(d *artifact.Descriptor) { d.Version = "5.24.2" }},
		{name: "operating system", mutate: func(d *artifact.Descriptor) { d.OS = "windows" }},
		{name: "architecture", mutate: func(d *artifact.Descriptor) { d.Architecture = "arm64" }},
		{name: "license", mutate: func(d *artifact.Descriptor) { d.License = "MIT" }},
		{name: "source URI", mutate: func(d *artifact.Descriptor) { d.SourceURI = "https://example.com/osquery" }},
		{name: "size", mutate: func(d *artifact.Descriptor) { d.SizeBytes++ }},
		{name: "SHA-256", mutate: func(d *artifact.Descriptor) {
			digest := []byte(d.SHA256)
			if digest[0] == '0' {
				digest[0] = '1'
			} else {
				digest[0] = '0'
			}
			d.SHA256 = string(digest)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := newFixtureSession(t, fixture)
			applyValidMetadataChain(t, session, fixture)
			descriptor := fixture.descriptor
			tc.mutate(&descriptor)
			if _, err := session.VerifyDescriptor(descriptor); !errors.Is(err, ErrTargetDescriptorMismatch) {
				t.Fatalf("VerifyDescriptor() error = %v, want ErrTargetDescriptorMismatch", err)
			}
		})
	}
}

func TestMetadataSessionRejectsInvalidOrMissingSignedDescriptor(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	applyValidMetadataChain(t, session, fixture)

	// The TUF signature authenticates custom metadata too. These parser cases
	// exercise the strict decoder independently of signature verification.
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "missing", raw: nil},
		{name: "duplicate key", raw: json.RawMessage(`{"artifactId":"one","artifactId":"two"}`)},
		{name: "unknown field", raw: json.RawMessage(`{"unexpected":true}`)},
		{name: "unpaired surrogate", raw: json.RawMessage(`{"artifactId":"\\ud800"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeArtifactDescriptor(tc.raw); !errors.Is(err, ErrInvalidTargetDescriptor) {
				t.Fatalf("decodeArtifactDescriptor() error = %v, want ErrInvalidTargetDescriptor", err)
			}
		})
	}

	targets := session.trusted.Targets[metadata.TARGETS]
	targets.Signed.Targets[fixture.targetPath].Custom = nil
	if _, err := session.VerifyDescriptor(fixture.descriptor); !errors.Is(err, ErrInvalidTargetDescriptor) {
		t.Fatalf("VerifyDescriptor() without custom descriptor error = %v, want ErrInvalidTargetDescriptor", err)
	}
}

func TestMetadataSessionRejectsInvalidExternalArtifactDescriptor(t *testing.T) {
	fixture := makeTUFRepositoryFixture(t, time.Now().UTC().AddDate(0, 0, 1))
	session := newFixtureSession(t, fixture)
	applyValidMetadataChain(t, session, fixture)

	descriptor := fixture.descriptor
	descriptor.Version = "not-semver"
	if _, err := session.VerifyDescriptor(descriptor); !errors.Is(err, ErrInvalidTargetDescriptor) {
		t.Fatalf("VerifyDescriptor() error = %v, want ErrInvalidTargetDescriptor", err)
	}
}

