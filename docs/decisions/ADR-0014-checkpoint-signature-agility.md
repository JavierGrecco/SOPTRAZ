# ADR-0014: Agilidad de firma de checkpoints — Ed25519 y ECDSA P-256

- Estado: Aceptada para el contrato criptográfico; adaptadores de hardware pendientes.
- Fecha: 2026-11-27
- Relacionada: ADR-0013, Issue #3

## Contexto

Field debe poder firmar y verificar checkpoints offline, y aprovechar claves protegidas cuando el sistema operativo o el hardware lo permita. Un contrato ligado exclusivamente a Ed25519 impediría integrar directamente varios proveedores de claves de hardware que exponen ECDSA P-256.

Al mismo tiempo, la forma de serializar ECDSA debe ser estable: una firma ASN.1 DER tiene longitud variable y ECDSA puede producir firmas diferentes para el mismo mensaje. El digest de cadena no debe depender de esos bytes aleatorios.

## Decisión

1. El contrato v1 admite exactamente `ed25519` y `ecdsa-p256-sha256`. Un algoritmo nuevo requiere actualización explícita del esquema y de los verificadores; no se aceptan downgrades ni algoritmos desconocidos.
2. Ed25519 usa la firma raw de 64 bytes definida por RFC 8032.
3. ECDSA P-256 usa SHA-256 y se transporta como 64 bytes IEEE P1363 `r || s`, Base64 estándar. Se exige low-S y `r`/`s` dentro de rango. Los adaptadores convierten cualquier firma nativa DER a P1363 antes de devolverla al núcleo.
4. `keyId` es SHA-256 hexadecimal en minúsculas del material público canónico: los 32 bytes raw de Ed25519 o el punto SEC1 uncompressed de P-256 (`0x04 || X || Y`).
5. `CheckpointSigner` devuelve la clave pública que corresponde a la operación privada. El núcleo calcula su identidad y verifica la firma recién generada contra esa clave antes de producir el objeto firmado.
6. `CheckpointKeyResolver` obtiene la clave pública desde un trust store independiente. La clave adjunta al bundle no es raíz de confianza y no se enrola automáticamente.
7. El preimage binario del checkpoint sigue determinista y separado por dominio. `CheckpointDigest` se calcula sobre el enunciado canónico, no sobre los bytes de firma, y solo se devuelve después de verificar la firma contra el trust store.
8. ECDSA P-256 se recomienda cuando el proveedor protegido lo soporte; Ed25519 sigue disponible para software o proveedores externos que lo implementen. SOPTRAZ detectará capacidades y no presumirá la presencia de TPM, Secure Enclave o PKCS#11.
9. La clave privada nunca entra en SQLite, eventos, logs, CLI, paquetes ni manifiestos. La custodia, el enrolamiento, la rotación y la revocación pertenecen a adaptadores y políticas independientes.

## Consecuencias

- Mismo checkpoint canónico produce el mismo digest aunque dos firmas ECDSA válidas sean diferentes.
- La firma y su `keyId` deben corresponder a una misma clave pública; un proveedor mal configurado falla cerrado.
- Los formatos y la política de low-S son parte del contrato y se prueban con fixtures.
- El núcleo no depende de Windows CNG/TPM, macOS Secure Enclave/Keychain ni Linux PKCS#11; esas son implementaciones de `CheckpointSigner` y `CheckpointKeyResolver`.
- El código actual incluye implementaciones software de referencia/test, no almacenes de claves protegidas de producción.
- ECDSA no implica por sí solo anti-rollback: sigue siendo necesario un trust anchor y un checkpoint anterior independiente según ADR-0013.

## Alternativas descartadas

- Solo Ed25519: no cubre todos los proveedores de claves hardware previstos.
- Transportar DER variable: dificulta la canonicalización, fixtures y tamaño fijo del contrato.
- Usar bytes de firma como digest de cadena: ECDSA puede generar firmas distintas del mismo enunciado.
- Confiar en la clave pública incluida en el paquete: el paquete podría sustituir contenido, clave y firma en conjunto.
- Fallback silencioso a software cuando no hay hardware: contradice la política de capacidad explícita y puede cambiar el modelo de custodia.

## Pruebas requeridas

- Ed25519 y P-256 válidas; algoritmo desconocido; clave pública con curva incorrecta.
- Firma alterada, truncada, base64 no canónica, ECDSA high-S y `r`/`s` fuera de rango.
- Clave privada distinta de la clave pública declarada por el signer.
- Digest estable para el mismo enunciado con firmas ECDSA diferentes; digest rechazado antes de verificación.
- Trust resolver ausente/desconocido, rotación y revocación.
- Windows, macOS y Linux con proveedores reales cuando se implementen.
