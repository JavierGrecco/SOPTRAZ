# Ediciones Field y Hub

## SOPTRAZ Field

- Ejecutable local/portable.
- Offline-first, sin servidor obligatorio.
- SQLite local con driver sin CGO.
- Diagnóstico e inventario del host conectado.
- Procedimientos y herramientas locales.
- Sesiones de intervención.
- Auditoría append-only con hash chain por intervención.
- Aprobación según riesgo.
- Informes HTML/PDF/JSON.
- Exportación de paquetes de intervención (pendiente).
- IA opt-in y fallback local.
- Sin agente residente obligatorio.

Field no guarda claves API ni secretos del cliente en el pendrive por defecto. Usa almacén seguro del SO o credenciales efímeras de sesión.

## SOPTRAZ Hub

- Instalación en servidor propio o infraestructura aprobada.
- Aplicación web multiusuario.
- Go como servicio/API.
- PostgreSQL.
- Organizaciones, sedes, grupos, equipos y usuarios.
- RBAC con alcance por organización/activo.
- Inventario y políticas centrales.
- Tareas, aprobaciones y SLA configurables.
- Auditoría consolidada con el mismo contrato lógico que Field.
- Conectores ITSM y endpoint.
- AI Gateway organizacional.
- Agente residente opcional y expresamente enrolado.

## Sincronización Field → Hub

- Field funciona sin Hub.
- Sincronización explícita, reanudable e idempotente.
- Paquete de intervención con ID, versión de esquema, hash y evidencias permitidas.
- Outbox/inbox.
- Conflictos resueltos por tipo de dato; no aplicar last-write-wins a auditoría.
- No sincronizar secretos o datos personales innecesarios.
- Evidencias grandes se transfieren separadamente con integridad verificable.

## Interoperabilidad

Field y Hub comparten contrato de intervención y modelos públicos. Una intervención Field se importa en Hub sin convertirse en otra implementación.
