package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

func replayFixtureRoles(t *testing.T, fixture delegatedRepositoryFixture) []sqlitetuf.VerifiedRoleMetadata {
	t.Helper()
	timestamp, err := metadata.Timestamp().FromBytes(fixture.metadata["timestamp.json"])
	if err != nil {
		t.Fatalf("parse fixture timestamp: %v", err)
	}
	snapshot, err := metadata.Snapshot().FromBytes(fixture.metadata["snapshot.json"])
	if err != nil {
		t.Fatalf("parse fixture snapshot: %v", err)
	}
	targets, err := metadata.Targets().FromBytes(fixture.metadata["targets.json"])
	if err != nil {
		t.Fatalf("parse fixture targets: %v", err)
	}
	delegated, err := metadata.Targets().FromBytes(fixture.metadata["osquery.json"])
	if err != nil {
		t.Fatalf("parse fixture delegated targets: %v", err)
	}
	return []sqlitetuf.VerifiedRoleMetadata{
		{Role: metadata.TIMESTAMP, Version: timestamp.Signed.Version, ExpiresAt: timestamp.Signed.Expires, Metadata: append([]byte(nil), fixture.metadata["timestamp.json"]...)},
		{Role: metadata.SNAPSHOT, Version: snapshot.Signed.Version, ExpiresAt: snapshot.Signed.Expires, Metadata: append([]byte(nil), fixture.metadata["snapshot.json"]...)},
		{Role: metadata.TARGETS, Version: targets.Signed.Version, ExpiresAt: targets.Signed.Expires, Metadata: append([]byte(nil), fixture.metadata["targets.json"]...)},
		{Role: "osquery", Parents: []string{metadata.TARGETS}, Version: delegated.Signed.Version, ExpiresAt: delegated.Signed.Expires, Metadata: append([]byte(nil), fixture.metadata["osquery.json"]...)},
	}
}

func replayFixtureRootState(fixture delegatedRepositoryFixture) sqlitetuf.RootState {
	return sqlitetuf.RootState{Version: 1, SHA256: rootPin(fixture.rootJSON)}
}

func TestReplayVerifiedGenerationUsesOneExplicitReferenceTime(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)
	referenceTime := time.Now().UTC().Add(-time.Second)

	replayed, err := replayVerifiedGeneration(
		fixture.rootJSON,
		replayFixtureRootState(fixture),
		referenceTime,
		roles,
		4,
	)
	if err != nil {
		t.Fatalf("replayVerifiedGeneration() error = %v", err)
	}
	if !replayed.RefTime.Equal(referenceTime) {
		t.Fatalf("replay reference time = %s, want %s", replayed.RefTime, referenceTime)
	}
	if replayed.Root == nil || replayed.Timestamp == nil || replayed.Snapshot == nil ||
		replayed.Targets[metadata.TARGETS] == nil || replayed.Targets["osquery"] == nil {
		t.Fatal("replay did not reconstruct all top-level and delegated roles")
	}
}

func TestReplayVerifiedGenerationRejectsExpiredMetadataAtExplicitReferenceTime(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)

	// Keep the root valid at the reference time so this exercises timestamp
	// expiry rather than failing earlier on the root's expiration.
	root, err := metadata.Root().FromBytes(fixture.rootJSON)
	if err != nil {
		t.Fatalf("parse fixture root: %v", err)
	}
	root.Signed.Expires = time.Now().UTC().Add(72 * time.Hour)
	rootJSON := signedMetadataBytes(t, root, fixture.rootSigner)
	rootState := sqlitetuf.RootState{Version: 1, SHA256: rootPin(rootJSON)}
	referenceTime := time.Now().UTC().Add(48 * time.Hour)

	if _, err := replayVerifiedGeneration(rootJSON, rootState, referenceTime, roles, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("replayVerifiedGeneration() error = %v, want ErrGenerationReplay", err)
	}
}
func TestReplayVerifiedGenerationRejectsWrongRootPinAndMissingTime(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)
	state := replayFixtureRootState(fixture)
	state.SHA256 = rootPin([]byte("different root"))

	if _, err := replayVerifiedGeneration(fixture.rootJSON, state, time.Now(), roles, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("wrong root pin error = %v, want ErrGenerationReplay", err)
	}
	if _, err := replayVerifiedGeneration(fixture.rootJSON, replayFixtureRootState(fixture), time.Time{}, roles, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("missing reference time error = %v, want ErrGenerationReplay", err)
	}
}

func TestReplayVerifiedGenerationRejectsDelegatedBytesNotBoundBySnapshot(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)
	var envelope struct {
		Signatures []struct {
			Sig string `json:"sig"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(roles[3].Metadata, &envelope); err != nil {
		t.Fatalf("decode delegated fixture signature metadata: %v", err)
	}
	if len(envelope.Signatures) == 0 || len(envelope.Signatures[0].Sig) == 0 {
		t.Fatal("delegated fixture has no signature")
	}
	mutated := append([]byte(nil), roles[3].Metadata...)
	signature := []byte(envelope.Signatures[0].Sig)
	index := bytes.Index(mutated, signature)
	if index < 0 {
		t.Fatal("could not locate exact delegated signature bytes")
	}
	if mutated[index] == '0' {
		mutated[index] = '1'
	} else {
		mutated[index] = '0'
	}
	roles[3].Metadata = mutated

	if _, err := replayVerifiedGeneration(fixture.rootJSON, replayFixtureRootState(fixture), time.Now().UTC(), roles, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("altered delegated bytes error = %v, want ErrGenerationReplay", err)
	}
}

func TestReplayVerifiedGenerationRejectsSnapshotBytesNotBoundByTimestamp(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)
	var envelope struct {
		Signatures []struct {
			Sig string `json:"sig"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(roles[1].Metadata, &envelope); err != nil {
		t.Fatalf("decode snapshot fixture signature: %v", err)
	}
	if len(envelope.Signatures) == 0 || envelope.Signatures[0].Sig == "" {
		t.Fatal("snapshot fixture has no signature")
	}
	mutated := append([]byte(nil), roles[1].Metadata...)
	signature := []byte(envelope.Signatures[0].Sig)
	index := bytes.Index(mutated, signature)
	if index < 0 {
		t.Fatal("could not locate exact snapshot signature bytes")
	}
	if mutated[index] == '0' {
		mutated[index] = '1'
	} else {
		mutated[index] = '0'
	}
	roles[1].Metadata = mutated

	if _, err := replayVerifiedGeneration(fixture.rootJSON, replayFixtureRootState(fixture), time.Now().UTC(), roles, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("snapshot bytes not bound by timestamp error = %v, want ErrGenerationReplay", err)
	}
}

func TestReplayVerifiedGenerationRejectsMissingParentAndCycles(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	roles := replayFixtureRoles(t, fixture)

	missing := append([]sqlitetuf.VerifiedRoleMetadata(nil), roles...)
	missing[3].Parents = nil
	if _, err := replayVerifiedGeneration(fixture.rootJSON, replayFixtureRootState(fixture), time.Now().UTC(), missing, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("missing parent error = %v, want ErrGenerationReplay", err)
	}

	cycle := append([]sqlitetuf.VerifiedRoleMetadata(nil), roles...)
	cycle = append(cycle, sqlitetuf.VerifiedRoleMetadata{
		Role:      "mirror",
		Parents:   []string{"osquery"},
		Version:   cycle[3].Version,
		ExpiresAt: cycle[3].ExpiresAt,
		Metadata:  append([]byte(nil), cycle[3].Metadata...),
	})
	cycle[3].Parents = []string{"mirror"}
	if _, err := replayVerifiedGeneration(fixture.rootJSON, replayFixtureRootState(fixture), time.Now().UTC(), cycle, 4); !errors.Is(err, ErrGenerationReplay) {
		t.Fatalf("cyclic graph error = %v, want ErrGenerationReplay", err)
	}
}
