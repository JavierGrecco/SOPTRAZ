# Arquitectura general

**Estado:** arquitectura inicial aceptada para la fundación. Cambios estructurales requieren ADR.

## Estilo arquitectónico

SOPTRAZ comienza como un **monolito modular**. Los procesos auxiliares se aíslan cuando exista una razón técnica. No se inicia con microservicios.

Cada módulo tiene responsabilidad, límite de datos e interfaz pública explícita. Los módulos no acceden directamente a estructuras internas de otros módulos ni comparten tablas sin contratos.

Field y Hub son dos composiciones del mismo producto, no dos bases de código de negocio.

## Capas

1. Presentación: UI escritorio/web y CLI.
2. Aplicación: casos de uso, orquestación de intervenciones y tareas.
3. Dominio: reglas, estados, riesgos, permisos y contratos.
4. Adaptadores: Windows, macOS, Linux, herramientas OEM y proyectos externos.
5. Persistencia: SQLite en Field; PostgreSQL en Hub.
6. Servicios transversales: auditoría, políticas, configuración, AI Gateway y actualizaciones.

La presentación no contiene lógica de reparación ni cálculos analíticos autoritativos.

## Flujo de intervención

Solicitud → Contexto del activo → Diagnóstico → Evidencias → Plan → Aprobación → Ejecución → Verificación → Informe → Cierre.

Cada paso produce eventos correlacionables mediante un ID de intervención. Los procedimientos declaran prerrequisitos, capacidades, permisos, riesgo, timeout, idempotencia y verificación.

## Módulos

- Core/Runtime: ciclo de vida, configuración, capacidades y dispatch.
- Diagnostics: recopilación de evidencia y hallazgos.
- Inventory: activos, hardware y software detectado.
- Driver Intelligence: diagnóstico y paquetes OEM en Windows.
- Network: conectividad, DNS y adaptadores.
- Storage: salud, sistemas de archivos, espacio y rescate.
- Repair: procedimientos de reparación del sistema.
- Recovery: entornos offline y herramientas de rescate.
- AI Gateway: proveedores, cuotas, privacidad y fallback local.
- Analytics Engine: análisis estadístico y modelos numéricos en Python.
- Organizations: organizaciones, sucursales, usuarios, equipos y roles.
- Tasks: asignaciones, estados, aprobaciones y seguimiento.
- Audit & Reports: eventos, evidencias e informes.
- Integration Hub: conectores de terceros y manifiestos.

## Comunicación

Dentro del proceso: interfaces Go pequeñas y dependencias explícitas. Entre procesos/lenguajes: JSON Schema versionado; OpenAPI para APIs HTTP. No compartir structs internos entre módulos.

Eventos: intervention.created, diagnostic.completed, approval.granted, operation.started, operation.completed, verification.failed, intervention.closed.

No usar eventos como sustituto de llamadas síncronas cuando se requiere respuesta inmediata.

## Ejecución segura

- El núcleo recibe acciones tipadas, nunca strings de shell libres.
- Cada acción resuelve a un procedimiento registrado y versionado.
- Parámetros validados y argumentos sin concatenación de shell.
- Capacidades mínimas y timeout explícito.
- Salida, stderr, exit code y duración capturados.
- Requisitos de administrador declarados.
- Consentimiento según política de riesgo.
- Cancelación y recuperación documentadas.
- Resultado verificado independientemente del exit code.

## Field y Hub

Field funciona localmente con SQLite y no requiere servidor. Hub incorpora servicios multiusuario y PostgreSQL. La sincronización se realiza mediante eventos/paquetes versionados y outbox/inbox idempotentes; nunca se copia una base SQLite abierta a través de red.

## Seguridad y compatibilidad

Cada módulo identifica versión, API compatible, plataformas, capacidades, permisos, dependencias, hashes, firma y licencia. Herramientas externas aisladas con capacidades permitidas. No se carga código no verificado.

La matriz de compatibilidad declara SO, versión, arquitectura y dependencias. Linux desktop tendrá alternativa web local para distribuciones sin el WebKit/GTK requerido por Wails.
