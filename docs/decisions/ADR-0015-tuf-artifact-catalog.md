# ADR-0015: Catálogo autenticado de artefactos con TUF/go-tuf v2

- Estado: Aceptada para la arquitectura; cliente y cache offline pendientes de implementación.
- Fecha: 2026-11-27
- Relacionada: ADR-0007, ADR-0009, ADR-0014, Issue #5 (catálogo), Issue #4 (supervisor).

## Contexto

El descriptor local artifact v1 declara versión, plataforma, ruta, SHA-256, tamaño, licencia y URL de procedencia. El resolver confirma que el archivo instalado coincide con esos metadatos, pero un hash no demuestra quién publicó el descriptor. Una firma ad hoc resolvería solo una parte: también necesitamos rotación de claves, umbrales, expiración, snapshots, delegaciones y protección contra rollback/freeze.

Field es offline-first. La confianza no puede depender de descargar una clave junto con el catálogo que esa misma clave pretende autenticar.

## Alternativas

1. Firmas propietarias por descriptor: criptografía sencilla, pero exige diseñar y mantener formato canónico, umbrales, rotación, revocación, expiración, snapshots y protección anti-rollback. Se descarta como solución principal.
2. Sigstore/Cosign como catálogo único: permite verificar firmas de blobs, identidad y attestations; es útil para provenance upstream, pero no sustituye por sí solo las semánticas de actualización y delegación TUF. La verificación keyless puede requerir identidad y transparencia externas.
3. The Update Framework (TUF) mediante go-tuf/v2: protocolo con roles, threshold de claves, rotación de root, expiración, snapshots, delegaciones y protecciones ante rollback/freeze; tiene implementación Go oficial.

## Decisión

1. La base del catálogo será github.com/theupdateframework/go-tuf/v2, versión evaluada v2.5.0, licencia Apache-2.0 y mínimo Go 1.27. Se descarta github.com/theupdateframework/go-tuf v0.x: su proyecto la declara legacy/deprecada y sin futuras correcciones.
2. La raíz root.json se incorporará como trust anchor fijado por la versión de SOPTRAZ o se instalará mediante enrolamiento explícito verificado fuera de banda. No se descargará una raíz desconocida con Trust-On-First-Use.
3. El repositorio usará roles root, timestamp, snapshot y targets; cuando aporte separación de privilegios, delegará targets por proveedor, familia o plataforma. La raíz tendrá umbral y claves custodiadas offline; claves de publicación delegadas tendrán privilegios mínimos. No se almacenarán claves privadas en repo, base Field, logs ni paquetes.
4. El catálogo vinculará target, versión, plataforma, licencia, tamaño y SHA-256. El descriptor artifact v1 seguirá siendo el contrato de aplicación y estará embebido íntegramente en `targets.custom`, cubierto por la firma TUF. SOPTRAZ exigirá coincidencia exacta del descriptor con el custom firmado y con ruta, longitud y SHA-256 del target. La metadata custom se decodificará con el contrato estricto (sin claves duplicadas, campos desconocidos ni escapes Unicode inválidos). El resolver os.Root verificará nuevamente el artefacto local.
5. Los metadatos incluirán expiración y versiones monotónicas. La actualización comprobará rotación consecutiva de root, threshold de firmas, snapshot/targets enlazados por hash, consistent snapshots y rechazo de rollback/freeze. La ausencia de red no convierte metadata vencida o no verificable en confiable.
6. El modo offline solo permitirá metadata previamente verificada y persistida atómicamente, todavía vigente, con raíz y estado anti-rollback comprobados. No se usará UpdaterConfig.UnsafeLocalMode como bypass: go-tuf documenta que ese modo se aparta del flujo de la especificación. La restauración de metadata y trust state tras reinicio se probará antes de habilitar uso offline del catálogo.
7. updater.Updater no es seguro para concurrencia. El adaptador SOPTRAZ deberá serializarlo con mutex o aislar instancias. El fetcher tendrá timeout HTTP finito, tamaño máximo por metadata/target, reintentos acotados y transporte controlado. No se heredará http.DefaultClient sin configuración explícita.
8. Cosign/Sigstore queda como adaptador complementario y opcional para verificar firmas, identidades y attestations upstream de releases públicas. No sustituye el trust root TUF ni implica dependencia online para validar artefactos locales ya autenticados.
9. TUF autentica catálogo y targets; no autoriza ejecución. Ningún target se ejecutará hasta que el supervisor M1.5 aplique allowlist de argumentos, permisos, timeout, aislamiento y auditoría.

## Consecuencias

- Se aprovecha una implementación oficial del protocolo en lugar de mantener criptografía y actualización propietarias.
- La primera capa online SOPTRAZ usa `updater.Updater` y `GetTargetInfo` oficiales para resolver delegaciones; limita el fetcher, bloquea redirects y serializa el Updater. Cada refresh crea una instancia nueva porque go-tuf permite un único refresh por instancia; un refresh fallido invalida el Updater en memoria. Las pruebas de integración usan un repositorio TUF de laboratorio y no descargan el binario target.
- Esta integración todavía requiere un `TrustAnchor` entregado externamente. No representa bootstrap de root de producción, persistencia anti-rollback completa ni soporte offline de producción.
- La integración añade dependencias transitivas relevantes, incluida Sigstore; se revisarán SBOM, licencias y avisos en cada actualización.
- go-tuf/v2.5.0 es reciente (publicado 2026-11-25): la adopción queda condicionada a pruebas de seguridad y matriz Windows/macOS/Linux.
- La cache offline, el trust anchor y el anti-rollback local son parte del modelo de seguridad; no se resuelven solo con guardar root.json.
- El descriptor artifact v1 no se convierte en launcher ni en permiso de ejecución.

## Pruebas de aceptación

- Root bootstrap no confiable/TOFU rechazado; root fijado válido aceptado.
- Rotación root N a N+1 con firmas anteriores y nuevas; salto de versión y clave no autorizada rechazados.
- Umbral de firma, firmas duplicadas, firma alterada y algoritmos no admitidos rechazados.
- Timestamp, snapshot o targets expirados; rollback y metadata inconsistente rechazados.
- Descriptor artifact v1 completo firmado en `targets.custom`; ID, versión, SO, arquitectura, licencia, source URI, ruta, tamaño y SHA-256 deben coincidir con el target.
- Hash y longitud de descriptor/binario verificados en todas las plataformas.
- Cache offline íntegra y vigente aceptada; cache modificada, incompleta, vencida o con versión menor rechazada.
- Reinicio, cierre abrupto y persistencia atómica probados.
- Updater concurrente protegido o aislado; timeout, respuestas excesivas y reintentos limitados.
- Windows, macOS, Ubuntu, Race Detector y pruebas de seguridad verdes.