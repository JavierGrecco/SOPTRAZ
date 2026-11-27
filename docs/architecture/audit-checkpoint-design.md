# Diseño de checkpoints firmados y exportación Field → Hub

**Estado:** especificación M2; firma y exportación/importación aún no implementadas.

## Objetivo y límite del ledger actual

El ledger SHA-256 actual detecta modificaciones parciales, huecos y truncamientos si la cabecera original permanece intacta. No puede detectar por sí solo una reescritura completa de SQLite con hashes recalculados.

M2 agregará una referencia criptográfica independiente a una cabeza concreta de la cadena y un paquete de intervención verificable. No llamaremos WORM a SQLite ni a un archivo transportado en un pendrive.

## Modelo de confianza

Se separan tres elementos:

1. **Ledger local:** eventos SQLite encadenados por intervención.
2. **Firma:** un proveedor autorizado firma un checkpoint. La clave privada no se guarda en SQLite, logs, bundle ni un archivo plano dentro del pendrive.
3. **Ancla independiente:** se conserva otra copia del checkpoint en otro dominio de fallo (Hub propio, otro medio bajo custodia separada o repositorio organizacional). El verificador obtiene las claves públicas desde una política/registro independiente del bundle.

Un bundle que lleva su propia clave pública y su propia firma solo demuestra consistencia interna; no prueba por sí mismo quién firmó. Una firma junto a la base, bajo la misma cuenta y en el mismo medio, tampoco impide que un mismo atacante sustituya ambas.

La verificación fuerte requiere una clave pública de confianza obtenida por canal independiente, validar firma y cabeza contra los eventos, y comparar la secuencia contra el último checkpoint independiente conocido para detectar rollback. Ante clave desconocida, firma inválida, downgrade, salto o fork se falla cerrado.

Sin ancla anterior o destino independiente, el resultado solo puede declarar integridad estructural de la cadena, no autenticidad ni anti-rollback.

## Checkpoint v1

Estructura cerrada y versionada, sin mapas JSON en el material firmado:

- checkpointSchemaVersion, checkpointId (UUID), interventionId (UUID)
- eventSchemaVersion, hashAlgorithm (sha256), signatureAlgorithm (ed25519)
- sequence, headHash, previousCheckpointHash
- createdAt (UTC, informativo; no es un timestamp confiable)
- keyId derivado de la clave pública confiable

La firma cubre una codificación binaria determinista del checkpoint sin el campo signature, precedida por un separador de dominio como `SOPTRAZ-AUDIT-CHECKPOINT-V1\0`. Orden y codificación no cambian dentro de v1. Un cambio incompatible crea una versión nueva y fixtures de compatibilidad.

Se propone Ed25519 de la biblioteca estándar Go `crypto/ed25519`; no se agrega una dependencia criptográfica externa. La capa de transporte valida longitud y algoritmo antes de verificar. keyId deriva de la clave pública mediante SHA-256 y se resuelve contra el trust store externo.

## Custodia y rotación

Interfaces de dominio previstas:

- `Signer.Sign(ctx, keyID, message) -> signature`
- `TrustStore.Resolve(keyID) -> publicKey + policy`
- `Verifier.Verify(checkpoint, signature, trustedPublicKey)`

El dominio no conoce DPAPI/CNG/TPM, Keychain ni libsecret; son futuros adaptadores. Nunca se guarda la clave privada en SQLite, logs o bundles. No se acepta una clave incluida en el bundle como raíz de confianza ni se enrola automáticamente al importarla. Alta, revocación y rotación pertenecen a un registro externo. Una rotación exige transición firmada por la clave anterior o autorización administrativa fuera de banda.

En el primer MVP se puede inyectar un signer de test/desarrollo. No se declara producción segura hasta integrar un proveedor protegido y un procedimiento de enrolamiento/recuperación para Windows, macOS y Linux.

## Ancla y anti-rollback

Cada checkpoint referencia el anterior mediante previousCheckpointHash e incrementa la secuencia por intervención.

Hub como ancla debe ser monotónico e idempotente:
- mismo checkpoint exacto repetido: no-op idempotente;
- checkpoint nuevo con previousCheckpointHash esperado y secuencia creciente: aceptar;
- checkpoint antiguo, bytes distintos con mismo ID, fork o salto sin prueba: rechazar y registrar conflicto.

Hub protege contra manipulación local, pero su operador sigue dentro del modelo de confianza. Quien necesite resistencia frente al propio operador puede replicar checkpoints a dominios independientes o usar un transparency log. Sigstore/Rekor es referencia de diseño (entradas append-only, raíz de Merkle firmada y pruebas de inclusión/consistencia); SOPTRAZ no dependerá de Rekor ni de servicios públicos para Field offline.

## Bundle portable .soptraz

ZIP versionado, sin enlaces simbólicos ni paths absolutos:

- `manifest.json`: versión, intervención, rango de eventos, hashes, schema y límites.
- `events.jsonl`: eventos originales ordenados, sin reescribir sequence/hash.
- `checkpoints/`: checkpoint y firma.
- `evidence/`: solo adjuntos seleccionados tras preview y aprobación.

Excluir secretos, tokens, credenciales, claves privadas, dumps de memoria y archivos personales. El manifiesto y hashes se canonicalizan; se fijan orden y serialización con golden fixtures. La firma autentica la cabeza del ledger; los hashes enlazan bytes exactos del bundle con el manifiesto.

## Importación segura

1. Validar magic/version/tamaño; rechazar paths absolutos, `..`, symlinks, duplicados y límites excesivos.
2. Verificar hashes del manifiesto y todos los archivos.
3. Validar eventos con JSON Schema.
4. Verificar secuencia continua, IDs, previousHash y cadena SHA-256.
5. Verificar firma con clave ya confiable.
6. Comparar con ancla conocida y comprobar rollback/fork.
7. Aplicar idempotencia: mismo bundle/hash exacto es no-op; mismo ID con bytes distintos es conflicto.
8. Confirmar en transacción solo después de todas las verificaciones.
9. Registrar origen, importador, tiempo y resultado.

Un paquete incompleto, modificado, firmado por clave no confiable o que retrocede la cabeza conocida no se importa parcialmente.

## Atomicidad y recuperación

La exportación se escribe a un archivo temporal en el mismo filesystem, se valida desde disco, se hace flush/fsync y se renombra atómicamente. No se borra el checkpoint anterior hasta confirmar el nuevo. La importación prepara y valida sin mutar el ledger; el commit final es atómico. Interrupción, disco lleno, permisos o cancelación deben dejar el estado previo utilizable.

## Pruebas de aceptación M2

- Ed25519 válida, inválida, bit cambiado, truncada y clave incorrecta.
- Trust anchor ausente y key ID desconocido.
- Alteración de evento, manifest, hash, secuencia, head y previous checkpoint.
- Reescritura total SQLite frente a checkpoint externo anterior.
- Rollback, fork, rotación/revocación.
- Duplicado exacto idempotente y mismo ID con bytes distintos rechazado.
- ZIP path traversal, symlink, duplicados, tamaño excesivo y compresión abusiva.
- Interrupción/disco lleno durante export/import.
- Windows, macOS, Linux y Race Detector.

## Fuera de alcance

Sincronización en tiempo real, agente residente, trust root global de SOPTRAZ, firma de releases, publicación obligatoria en transparencia pública o firma automática por IA.

## Referencias

- Go crypto/ed25519: https://pkg.go.dev/crypto/ed25519
- RFC 8032: https://www.rfc-editor.org/rfc/rfc8032
- OWASP Logging Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
- NIST SP 800-92: https://csrc.nist.gov/pubs/sp/800/92/final
- Sigstore Rekor Security Model: https://docs.sigstore.dev/about/security/
- Rekor architecture spec: https://github.com/sigstore/architecture-docs/blob/main/rekor-spec.md
