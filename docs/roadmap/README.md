# Roadmap de SOPTRAZ

El roadmap prioriza una vertical funcional confiable antes de ampliar herramientas.

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
- [ ] Completar ciclo de producción y resiliencia del updater: persistencia durable del estado anti-rollback, recuperación tras reinicio y política de actualización de root cacheado.
- [ ] Definir modo offline que revalide metadata cacheada, vigente y anti-rollback; no usar TOFU ni UnsafeLocalMode como bypass.
- [ ] Probar rotación root, thresholds, firma inválida, expiración, rollback/freeze, consistent snapshots, delegaciones y cache corrupta.
- [x] Configurar el fetcher inicial con timeout HTTP, límites de metadata, redirects desactivados, reintentos acotados y sincronización del Updater.
- [ ] Probar expiración/rollback/rotación y persistencia cacheada en escenarios de reinicio, caída de red y recuperación.
- [ ] Revisar SBOM, licencia Apache-2.0 y dependencias transitivas de go-tuf v2.5.0.

## M1.5 — Supervisor seguro de procesos y registro de módulos

- [x] Resolver artefactos instalados desde una raíz administrada con plataforma/arquitectura reales exactas, tamaño declarado exacto y SHA-256.
- [x] Rechazar traversal, symlinks y componentes no portables/reservados de Windows.
- [ ] Consumir únicamente targets autenticados del catálogo TUF (Issue #5).
- [ ] Implementar supervisor de procesos sin shell, con acciones y argumentos registrados y entorno mínimo.
- [ ] Implementar terminación del árbol de procesos y cancelación por sistema operativo.
- [ ] Aplicar timeout, límites de stdout/stderr, validación de salida y auditoría de ejecución.
- [ ] Probar timeout, cancelación, procesos hijos, salida excesiva y sustitución de binario en Windows, macOS y Linux.

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
