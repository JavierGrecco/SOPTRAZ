package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

// replayStoredRootChain authenticates every persisted root from the independent
// bootstrap anchor. A valid SQLite hash chain alone is not a trust anchor.
func replayStoredRootChain(
	bootstrapRootJSON []byte,
	bootstrapState TrustState,
	records []sqlitetuf.RootRecord,
) ([]byte, TrustState, error) {
	trusted, err := trustedmetadata.New(bootstrapRootJSON)
	if err != nil {
		return nil, TrustState{}, fmt.Errorf("abrir trust anchor con go-tuf: %w", err)
	}
	currentRootJSON := append([]byte(nil), bootstrapRootJSON...)
	currentState := bootstrapState
	for index, record := range records {
		if record.Version != currentState.Version+1 || record.PreviousSHA256 != currentState.SHA256 {
			return nil, TrustState{}, fmt.Errorf("root persistida %d: secuencia o enlace previo inválido", index+1)
		}
		if len(record.Metadata) == 0 || int64(len(record.Metadata)) > MaxRootMetadataBytes {
			return nil, TrustState{}, fmt.Errorf("root persistida v%d: tamaño fuera del límite", record.Version)
		}
		parsedRoot, err := metadata.Root().FromBytes(record.Metadata)
		if err != nil {
			return nil, TrustState{}, fmt.Errorf("parsear root persistida v%d: %w", record.Version, err)
		}
		if parsedRoot.Signed.Version != record.Version {
			return nil, TrustState{}, fmt.Errorf("root persistida: ruta v%d pero metadata v%d", record.Version, parsedRoot.Signed.Version)
		}
		rotated, err := trusted.UpdateRoot(record.Metadata)
		if err != nil {
			return nil, TrustState{}, fmt.Errorf("go-tuf rechazó root persistida v%d: %w", record.Version, err)
		}
		if rotated.Signed.Version != record.Version {
			return nil, TrustState{}, fmt.Errorf("go-tuf confirmó root v%d, se esperaba v%d", rotated.Signed.Version, record.Version)
		}
		digest := sha256.Sum256(record.Metadata)
		digestHex := hex.EncodeToString(digest[:])
		if digestHex != record.SHA256 {
			return nil, TrustState{}, fmt.Errorf("root persistida v%d: SHA-256 distinto al registro", record.Version)
		}
		currentRootJSON = append(currentRootJSON[:0], record.Metadata...)
		currentState = TrustState{Version: record.Version, SHA256: digestHex}
	}
	return currentRootJSON, currentState, nil
}

// verifyCapturedRootRotations replays exact root responses through go-tuf and
// returns only the valid consecutive prefix. A later bad response must not
// erase an earlier root already accepted by the official updater.
func verifyCapturedRootRotations(
	currentRootJSON []byte,
	currentState TrustState,
	candidates []capturedRootMetadata,
) ([]sqlitetuf.VerifiedRoot, []byte, TrustState, error) {
	if len(candidates) == 0 {
		return nil, append([]byte(nil), currentRootJSON...), currentState, nil
	}
	trusted, err := trustedmetadata.New(currentRootJSON)
	if err != nil {
		return nil, append([]byte(nil), currentRootJSON...), currentState, fmt.Errorf("abrir última root verificada: %w", err)
	}
	verified := make([]sqlitetuf.VerifiedRoot, 0, len(candidates))
	latestRootJSON := append([]byte(nil), currentRootJSON...)
	latestState := currentState
	for index, candidate := range candidates {
		if candidate.version != latestState.Version+1 {
			return verified, latestRootJSON, latestState, fmt.Errorf(
				"root recibida v%d fuera de secuencia; se esperaba v%d", candidate.version, latestState.Version+1,
			)
		}
		if len(candidate.metadata) == 0 || int64(len(candidate.metadata)) > MaxRootMetadataBytes {
			return verified, latestRootJSON, latestState, fmt.Errorf("root recibida v%d excede el límite", candidate.version)
		}
		parsedRoot, err := metadata.Root().FromBytes(candidate.metadata)
		if err != nil {
			return verified, latestRootJSON, latestState, fmt.Errorf("parsear root recibida v%d: %w", candidate.version, err)
		}
		if parsedRoot.Signed.Version != candidate.version {
			return verified, latestRootJSON, latestState, fmt.Errorf(
				"respuesta solicitada como root v%d contiene metadata v%d", candidate.version, parsedRoot.Signed.Version,
			)
		}
		rotated, err := trusted.UpdateRoot(candidate.metadata)
		if err != nil {
			return verified, latestRootJSON, latestState, fmt.Errorf("go-tuf rechazó root recibida v%d (posición %d): %w", candidate.version, index+1, err)
		}
		if rotated.Signed.Version != candidate.version {
			return verified, latestRootJSON, latestState, fmt.Errorf("go-tuf produjo root v%d al validar respuesta v%d", rotated.Signed.Version, candidate.version)
		}
		previousSHA256 := latestState.SHA256
		digest := sha256.Sum256(candidate.metadata)
		digestHex := hex.EncodeToString(digest[:])
		verified = append(verified, sqlitetuf.VerifiedRoot{
			Version:        candidate.version,
			PreviousSHA256: previousSHA256,
			Metadata:       append([]byte(nil), candidate.metadata...),
		})
		latestRootJSON = append(latestRootJSON[:0], candidate.metadata...)
		latestState = TrustState{Version: candidate.version, SHA256: digestHex}
	}
	return verified, latestRootJSON, latestState, nil
}