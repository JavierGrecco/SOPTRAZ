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

M1.4 permanece **abierto**. El estado real, el orden de implementación, los resultados CI, las ramas y los criterios para retirar una tarea están centralizados en la [guía de trabajo activo y continuidad](active-work.md). No mantener una segunda checklist técnica aquí.

**Hitos integrados en main:** cliente online oficial con go-tuf v2.5.0; root-chain persistente desde un pin bootstrap; store SQLite v2 de generaciones, high-water y delegaciones; captura/replay online de metadata; staging aislado por refresh; y snapshot SQLite consistente de lectura offline (PR #49). El snapshot todavía no autentica firmas ni habilita offline.

**Trabajo en revisión:** PR #50 (draft) comparte el replay criptográfico, añade resolución canónica de delegaciones según go-tuf y una primitiva interna para revalidar metadata desde un snapshot SQLite sin red. No debe confundirse con `OfflineCatalogVerifier`: siguen pendientes la integración de la política temporal ADR-0020, la orquestación final, el binding del descriptor artifact v1 y la aceptación de seguridad. El catálogo no descarga, instala ni ejecuta binarios.

## M1.5 — Supervisor seguro de procesos y registro de módulos

Contrato de ejecución: [perfil de ejecución de procesos](../architecture/process-execution-profile.md).

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
