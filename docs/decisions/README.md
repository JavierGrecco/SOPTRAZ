# Registro de decisiones arquitectónicas (ADR)

Las decisiones aceptadas se mantienen aquí. Una ADR registra contexto, alternativas, decisión, consecuencias y estado. No se reemplaza silenciosamente: se crea una ADR que la superseda.

| ID | Decisión | Estado |
|---|---|---|
| ADR-0001 | Monolito modular inicial | Aceptada |
| ADR-0002 | Go + Wails v3 + TypeScript/React | Aceptada para prototipo |
| ADR-0003 | Núcleo canónico por dominio; Python para analítica | Aceptada |
| ADR-0004 | SQLite Field y PostgreSQL Hub | Aceptada como dirección |
| ADR-0005 | IA gratuita primero, failover y modelo local | Aceptada |
| ADR-0006 | Procedimientos tipados y auditables | Aceptada |
| ADR-0007 | Integrar herramientas maduras | Aceptada |
| ADR-0008 | Field y Hub con contratos compartidos | Aceptada |
| ADR-0009 | JSON Schema Draft 2020-12 para contratos | Aceptada |
| ADR-0010 | Ciclo de vida explícito de intervenciones | Aceptada |
| ADR-0011 | Driver SQLite sin CGO para Field | Aceptada para primera implementación |
| ADR-0012 | Registro append-only con hash chain por intervención | Aceptada |
| ADR-0013 | Checkpoints firmados y anclados fuera de Field | Aceptada; implementación parcial |
| ADR-0014 | Agilidad de firma: Ed25519 y ECDSA P-256 | Aceptada para el contrato criptográfico; adaptadores protegidos pendientes |
| ADR-0015 | Catálogo de artefactos TUF con go-tuf/v2 | Primera capa online con targets delegados implementada; root productivo y offline pendientes |
| ADR-0016 | Optimización y mantenimiento modular de Windows | Aceptada como dirección; implementación pendiente |
| ADR-0017 | Almacén durable de confianza TUF en SQLite | Aceptada como decisión de almacenamiento; implementación pendiente |
| ADR-0018 | Informes locales de diagnóstico y recuperación de ejecución | Aceptada; implementación propuesta en PR #31, aún en revisión |
| ADR-0019 | Verificación offline explícita del catálogo TUF | Aceptada como arquitectura; implementación pendiente |

Pendiente: licencia de distribución, cifrado local, firmas de releases, packaging Python, sincronización e identidad antes de implementar esas funciones.
