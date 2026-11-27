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
- [x] Especificar el diseño de checkpoints firmados y anclaje independiente (ADR-0013).
- [ ] Implementar checkpoints firmados fuera de la base.
- [ ] Completar paquetes de intervención y sincronización Field → Hub.
- [ ] Definir ADR de firma de releases y política de distribución.

## M2 — Checkpoints y paquetes de intervención

- [x] Definir el modelo de checkpoint Ed25519 y sus límites de confianza.
- [x] Definir trust anchor independiente y anti-rollback.
- [x] Definir el formato lógico del paquete `.soptraz` y las reglas de importación atómica.
- [ ] Implementar contratos, signer, trust store y verificador.
- [ ] Implementar exportación/importación idempotente Field → Hub.
- [ ] Incorporar proveedores de claves protegidas por sistema operativo.
- [ ] Validar restauración, rotación/revocación y conflictos en los tres sistemas.

## Fase 1 — Core y auditoría

Runtime Go, registro local, IDs, acciones tipadas, permisos/riesgo, cancelación, timeout, SQLite y migraciones. La validación de contratos se utiliza como puerta de entrada. Completar checkpoints externos, recuperación ante corrupción, exportación verificable y retención.

## Fase 2 — Diagnóstico de solo lectura

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
