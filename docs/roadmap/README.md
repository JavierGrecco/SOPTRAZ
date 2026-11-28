# Roadmap de SOPTRAZ

El roadmap prioriza una vertical funcional confiable antes de ampliar herramientas.

**Plan director:** [estrategia de entrega, estado, puertas de aceptación y mapa de integraciones](strategy-and-integration-plan.md).

## Fase 0 — Fundación

- [x] Crear repositorio y README.
- [x] Definir principios, ediciones y arquitectura inicial.
- [x] Definir estrategia tecnológica por dominio.
- [x] Definir política Free-first de IA.
- [x] Crear CLI Go de solo lectura.
- [x] Agregar pruebas iniciales y CI multiplataforma.
- [x] Definir esquemas JSON para módulos, acciones, intervenciones y eventos.
- [x] Añadir validador Go de contratos.
- [x] Implementar y probar transiciones del ciclo de intervención.
- [x] Definir interfaz de auditoría y adaptador SQLite Field.
- [x] Implementar hash chain por intervención, triggers append-only y verificación.
- [x] Exponer una instantánea verificada de eventos y cabecera en una única transacción de lectura.
- [x] Especificar el diseño de checkpoints firmados y anclaje independiente (ADR-0013).
- [x] Implementar primitivas de checkpoint Ed25519 y ECDSA P-256 con firma verificada contra la clave pública declarada por el signer.
- [x] Implementar descriptor de artefacto v1 con JSON Schema, fixture y validación CLI; exigir SemVer, SHA-256 y tamaño exacto.
- [x] Implementar resolver de artefactos instalados con `os.Root`, plataforma real exacta, ruta portable, tamaño y hash, sin descargas ni ejecución.
- [x] Fijar Go `1.27.4` y probar rechazo de rutas de artefacto terminadas en `/`.
- [ ] Implementar almacenes de claves protegidas y enrolamiento por sistema operativo.
- [ ] Completar paquetes de intervención y sincronización Field → Hub.
- [ ] Definir ADR de firma de releases y política de distribución.

## M2 — Checkpoints y paquetes de intervención

- [x] Definir el modelo de checkpoint con Ed25519 y ECDSA P-256 y sus límites de confianza.
- [x] Definir trust anchor independiente y anti-rollback.
- [x] Definir el formato lógico del paquete `.soptraz` y las reglas de importación atómica.
- [x] Implementar contrato de checkpoint v1, preimagen binaria determinista, firma Ed25519/ECDSA P-256 P1363/low-S y resolver de clave externo.
- [x] Crear checkpoints desde `VerifiedSnapshot` SQLite en una transacción de lectura consistente.
- [x] Validar estructuralmente checkpoints desde la CLI y admitir ambas firmas en manifiestos `stable`.
- [ ] Implementar almacenes de claves protegidas y enrolamiento por sistema operativo.
- [ ] Implementar exportación/importación idempotente Field → Hub.
- [ ] Incorporar proveedores de claves protegidas por sistema operativo.
- [ ] Validar restauración, rotación/revocación y conflictos en los tres sistemas.

## Fase 1 — Core y auditoría

Runtime Go, registro local, IDs, acciones tipadas, permisos/riesgo, cancelación, timeout, SQLite y migraciones. La validación de contratos se utiliza como puerta de entrada. Completar checkpoints externos, recuperación ante corrupción, exportación verificable y retención.

## M1.4 — Catálogo autenticado de artefactos

- [x] Seleccionar TUF/go-tuf v2 por ADR-0015; descartar el módulo legacy v0.x.
- [ ] Fijar root.json inicial en SOPTRAZ y definir threshold, rotación y revocación de claves.
- [x] Validar la cadena root fijada → timestamp → snapshot → targets y rechazar metadata fuera de orden, vencida, alterada o excesiva (sesión determinista de pruebas; aún no es cliente de producción).
- [x] Vincular descriptor artifact v1 completo en `targets.custom` con igualdad exacta de ID, versión, SO/arquitectura, licencia, procedencia, ruta, tamaño y SHA-256 frente al target firmado.
- [x] Incorporar primera capa online `TUFClient` sobre el `updater.Updater` oficial: resuelve roles delegados, valida descriptor firmado y serializa accesos; límites HTTP/metadata, redirects desactivados y pruebas de integración con repo TUF delegado. No descarga ni ejecuta binarios.
- [ ] Completar bootstrap/enrolamiento de root de producción y política de threshold/rotación/revocación; el cliente actual recibe un `TrustAnchor` ya fijado.
- [x] Seleccionar almacenamiento durable para root chain, generaciones, high-water y active generation mediante SQLite separado (ADR-0017).
- [x] Implementar la primera capa aislada del store SQLite para la cadena root: anchor/origen fijados, raíces append-only, enlaces SHA-256, versiones consecutivas, CAS transaccional, lectura consistente y reapertura (PR #22; CI completo de cinco jobs).
- [x] Integrar el store con `NewPersistentTUFClient`: replay de toda la cadena desde el pin bootstrap, captura de respuestas `N.root.json`, confirmación de que el updater oficial aceptó la misma versión y commit CAS durable incluso si falla metadata posterior (PR #23 integrado en `1988db9`; CI pre-merge 5/5 en run 46093570302).
- [x] Implementar el esquema SQLite v2 para generaciones inmutables, metadata por rol/delegación, manifiesto SHA-256, high-water por rol con procedencia inmutable y puntero `active_generation` (PR #25 integrado en `e4341ab`).
- [x] Implementar `CommitGeneration`/`LoadActiveGeneration`: root binding, CAS, commit atómico de blobs/relaciones/high-water/puntero y readback con verificación de hashes y manifiesto (PR #25; CI 5/5 en run 46095086164).
- [x] Conectar `TUFClient` con `CommitGeneration`: capturar los bytes exactos cacheados por go-tuf, contrastarlos con el estado confiable en memoria, reproducir timestamp/snapshot/targets y las delegaciones recorridas con `trustedmetadata`, y activar mediante CAS transaccional (PR #27 integrado como `4983f7d`; CI pre-merge y post-merge 5/5).
- [x] Aislar cada refresh persistente en un workspace privado y rehidratarlo desde la generación SQLite activa cuando coincide con la root vigente; el updater online oficial revalida la metadata y un fallo descarta el stage sin cambiar la generación confirmada. `TUFClient.Close()` elimina el workspace (PR #29). Esto no habilita offline.
- [ ] Completar resiliencia: terminación abrupta real y limpieza segura de stages huérfanos, recuperación tras caída en cada etapa, disco lleno, lock y escritores multi-proceso, y política de actualización de metadata no-root cacheada.
- [x] Definir el contrato puro `internal/platform/trustedtime`: intervalo de incertidumbre, procedencia, ancla, identidad de clave y SHA-256 de la atestación, sin I/O ni dependencias externas.
- [x] Definir el DTO puro `SystemTimeDiagnostics` separado de `Evidence`, con estados independientes de servicio/sincronización, unknown explícito y timestamps UTC canónicos.
- [x] Implementar el lector Linux read-only del estado del kernel con `golang.org/x/sys/unix.Adjtimex` y `Modes=0`; probar mapeo de estados, contexto cancelado, errores sanitizados y que el código no solicita ajustes. No se obtiene identidad de servidor ni hora de última sincronización.
- [x] Implementar consulta read-only del estado de W32Time mediante Service Control Manager con SC_MANAGER_CONNECT y SERVICE_QUERY_STATUS; sin elevación, UAC, w32tm ni controles sobre el servicio.
- [ ] Mantener SynchronizationState=unknown en Windows hasta disponer de una señal no privilegiada y fiable de sincronización efectiva; no inferirla del estado activo de W32Time.
- [ ] Implementar diagnóstico macOS timed sin leer estado cacheado privado; sincronización unknown hasta disponer de API pública verificable.
- [ ] Auditar POC aisladas de NTS/Roughtime, política de operadores independientes, anclas y rotación antes de seleccionar dependencias.
- [ ] Integrar evidencia aceptada con la política de recuperación online, persistencia SQLite v3 y un mecanismo de referencia temporal compatible con go-tuf.
- [ ] Implementar modo offline estricto que revalide metadata cacheada, vigente y anti-rollback con evidencia temporal suficiente. Si offline falla y hay red, descartar la sesión y repetir online desde cero; nunca aceptar metadata expirada ni mezclar resultados.
- [ ] Mantener cualquier modo histórico de metadata expirada separado, solo de auditoría y sin autorizar instalación/ejecución.
- [ ] Probar rotación root, thresholds, firma inválida, expiración, rollback/freeze, consistent snapshots, delegaciones, cache corrupta, recuperación de reloj y fallback online completo tras un fallo offline.
- [x] Configurar el fetcher inicial con timeout HTTP, límites de metadata, redirects desactivados, reintentos acotados y sincronización del Updater.
- [ ] Probar expiración/rollback/rotación y persistencia cacheada en reinicios, caída de red, recuperación temporal y `TrustedTimeUnavailable` cuando no haya evidencia suficiente.
- [ ] Revisar SBOM, licencia Apache-2.0 y dependencias transitivas de go-tuf v2.5.0.

## M1.5 — Supervisor seguro de procesos y registro de módulos

- [x] Definir contrato versionado para perfiles declarativos de procesos de solo lectura, con argv fijo, sin campos de ruta ejecutable, shell, entorno arbitrario ni directorio de trabajo.
- [x] Validar semánticamente límites de tiempo, argumentos, cuotas de salida, formatos y códigos de salida; rechazar elevación, aprobación, red y modos distintos de direct-argv.
- [ ] Implementar runner multiplataforma; validar antes cada artefacto contra targets autenticados TUF y bytes instalados.
- [x] Resolver artefactos instalados desde una raíz administrada con plataforma/arquitectura reales exactas, tamaño declarado exacto y SHA-256.
- [x] Rechazar traversal, symlinks y componentes no portables/reservados de Windows.
- [ ] Consumir únicamente targets autenticados del catálogo TUF (Issue #5).
- [ ] Implementar supervisor de procesos sin shell, con acciones y argumentos registrados y entorno mínimo.
- [ ] Implementar terminación del árbol de procesos y cancelación por sistema operativo.
- [ ] Aplicar timeout, límites de stdout/stderr, validación de salida y auditoría de ejecución.
- [ ] Probar timeout, cancelación, procesos hijos, salida excesiva y sustitución de binario en Windows, macOS y Linux.

## M1.6 — Optimización y mantenimiento modular de Windows

La vertical está registrada en [Issue #9](https://github.com/JavierGrecco/SOPTRAZ/issues/9) y definida por [ADR-0016](../decisions/ADR-0016-windows-optimization.md). Su alcance no adelanta la ejecución: las acciones mutantes dependen de M1.4 y M1.5.

- [x] Investigar y registrar fuentes candidatas, licencias, límites y modelo de integración.
- [x] Decidir arquitectura por catálogo declarativo propio, perfiles revisables y captura del estado real anterior (ADR-0016).
- [x] Documentar perfil adaptativo por form factor, capacidades, estado dinámico, evidencia y confianza: [windows-device-profile.md](../architecture/windows-device-profile.md).
- [ ] Definir JSON Schema/contrato de acción de optimización, incluidos compatibilidad por build/arquitectura, privilegios, riesgo, idempotencia, verificación y rollback exacto/parcial/no disponible.
- [x] Implementar la primera capa de inventario Windows read-only: parser SMBIOS seguro, chasis, alimentación/batería, touch/pen, postura convertible/dock cuando aplica, clasificador con confianza y CLI `soptraz device-profile [--json]`.
- [ ] Completar el baseline Windows read-only: DeviceForm/PCSystemType, teclado físico completo, GPU dedicada, sensores térmicos, almacenamiento/SMART, contexto VM/Remote y manejo de fuentes parciales, sin elevación innecesaria.
- [ ] Integrar el informe de dispositivo en la UI Wails/Field con vista touch-first para tablets y convertibles.
- [ ] Auditar tweaks candidatos de WinUtil y Sophia individualmente; conservar código solo cuando licencia, soporte, side effects y restauración sean aceptables.
- [ ] Habilitar la primera acción mutante de bajo riesgo solo detrás de M1.5, con estado previo, consentimiento, verificación y prueba en VM.
- [ ] Implementar perfiles Mínimo, Equilibrado, Gaming, Batería, Privacidad y Personalizado como selecciones revisables de acciones compatibles, no como scripts opacos.
- [ ] Incorporar accesos rápidos/menú extendido propio en Field después de que existan acciones seguras; la UI no contendrá lógica de sistema.
- [ ] Validar Windows 10/11, builds, ediciones y x64/ARM64 según soporte de cada acción; probar reinicio, conflictos externos y rollback.
- [ ] Medir antes/después antes de afirmar mejoras de FPS, latencia, RAM o disco.

## Fase 2 — Diagnóstico de solo lectura

Completar primero M1.4 (catálogo TUF autenticado) y M1.5 (supervisor de procesos restringido): ruta del ejecutable resuelta desde manifiesto verificado, hash/versión, allowlist de argumentos tipados sin shell, timeout, cancelación, límites de salida, códigos de salida estructurados y registro de auditoría. Una vez probado, integrar osquery como primera POC opcional con consultas predefinidas, extensiones deshabilitadas y artefactos oficiales fijados por SO/arquitectura/hash. No ejecutar SQL libre ni instalar un servicio residente en esta etapa.

Plataforma, inventario, disco/SMART, CPU/RAM, espacio, red/DNS, eventos, impresoras y primer informe.

## Fase 3 — AI Gateway

Proveedores, free-only, cuotas, cooldown, failover, Ollama, anonimización, preview y JSON Schema.

## Fase 4 — Driver Intelligence Windows

Hardware IDs, inventario, SetupAPI.dev.log, fuentes OEM, firma/compatibilidad, exportación, instalación aprobada y rollback.

## Fase 5 — Reparación y recuperación

Windows guiado, red/impresión, WinRE/WinPE, SystemRescue, Ventoy, Rescuezilla, rescate de datos, diagnóstico nativo macOS/Linux.

## Fase 6 — Field

UI Wails, sesiones, diagnóstico guiado, herramientas, informes y paquetes portables por SO/arquitectura.

## Fase 7 — Hub

API/UI web, PostgreSQL, organizaciones, usuarios, roles, equipos, activos, tareas, aprobaciones y sincronización.

## Fase 8 — Integraciones

GLPI, Fleet/osquery, OEM, Action1/PDQ opcionales, automatización y agentes opt-in.

## Puerta de salida

Alcance documentado, contratos definidos, aceptación verde, compatibilidad actualizada, riesgos/rollback, licencias revisadas y evidencia de funcionamiento.
