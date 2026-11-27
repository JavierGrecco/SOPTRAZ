# ADR-0013: Checkpoints de auditoría firmados y anclados fuera de Field

- Estado: Aceptada como dirección de diseño; implementación pendiente.
- Fecha: 2026-11-27

## Contexto

El ledger actual encadena eventos mediante SHA-256 y protege filas contra operaciones SQL ordinarias. Detecta muchas modificaciones parciales, pero no puede detectar por sí solo una reescritura completa de SQLite por quien controla el archivo.

## Decisión

1. Introducir checkpoint firmado por intervención que comprometa sequence, headHash, versión de esquema, algoritmo, ID y relación con checkpoint anterior.
2. Usar Ed25519 de la biblioteca estándar Go, con codificación determinista y separador de dominio versionado.
3. Mantener Signer y TrustStore como interfaces; DPAPI/CNG/TPM, Keychain y Linux key store serán adaptadores.
4. No almacenar la clave privada en SQLite ni incluirla en bundles.
5. Confiar solo en claves públicas enroladas por un canal independiente del paquete.
6. Conservar una copia en un ancla fuera del almacén verificado (Hub propio, medio separado o servicio de transparencia opcional).
7. Mantener avance monotónico e idempotencia; rechazar rollback/fork.
8. Sin ancla o trust anchor el resultado es integridad estructural, nunca autenticidad/anti-rollback.

## Alternativas descartadas

- Solo SHA-256 local: no detecta una reescritura total con hashes recalculados.
- Checkpoint y clave de confianza junto a SQLite/en el mismo bundle: el atacante puede sustituir ambos.
- Clave privada embebida en el binario: puede extraerse y comprometer todas las instalaciones.
- Firma sin checkpoint anterior independiente: autentica un estado, pero no demuestra ausencia de rollback.
- Transparency log público obligatorio: contradice el modo Field offline-first.

## Consecuencias

- Field sigue funcionando offline con ledger local.
- Verificación fuerte exige clave confiable y checkpoint fuera del medio/DB.
- Custodia, enrolamiento, recuperación y rotación deben definirse antes de producción.
- Import/export requiere atomicidad, límites de paquete, protección frente a ZIP path traversal y pruebas de replay/fork.
- Se toman como referencia OWASP (detección de manipulación y copias en medios separados) y Sigstore Rekor (checkpoints firmados e inclusión/consistencia verificable).
- Esta ADR no declara WORM, no crea timestamp confiable ni vuelve inmutable al administrador de Hub.

## Próximas decisiones

- Proveedor de claves por SO.
- Trust registry y ciclo de vida/revocación.
- Formato ZIP y límites.
- Método de anclaje offline alternativo a Hub.
