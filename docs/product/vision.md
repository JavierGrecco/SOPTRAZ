# Visión del producto

**Estado:** dirección inicial aprobada; funcionalidades pendientes de implementación y validación.

## Propósito

SOPTRAZ es una plataforma modular para diagnóstico, soporte, reparación, recuperación y gestión de operaciones IT. Convierte el trabajo técnico en intervenciones consistentes, explicables, verificables y auditables.

No es solamente un lanzador de comandos, un RMM, un ITSM ni un medio de arranque. Coordina herramientas y procedimientos especializados alrededor de una intervención completa.

## Ciclo de trabajo

1. Registrar solicitud o problema.
2. Identificar equipo y contexto.
3. Recopilar evidencias con seguridad.
4. Diagnosticar y priorizar hipótesis.
5. Recomendar procedimientos compatibles.
6. Solicitar autorización según riesgo.
7. Ejecutar operaciones controladas.
8. Verificar resultados.
9. Documentar y cerrar intervención.
10. Reutilizar conocimiento.

## Dos ediciones, un producto

### SOPTRAZ Field

Edición portátil, local y offline-first para técnicos. Funciona sin servidor y sin IA cloud. Almacena sesiones localmente, ejecuta diagnósticos de solo lectura, guía reparaciones autorizadas y produce informes exportables.

### SOPTRAZ Hub

Edición integrada para organizaciones. Añade usuarios, roles, equipos de trabajo, organizaciones, sucursales, activos, tareas, políticas, automatizaciones, auditoría centralizada y conectores empresariales.

Ambas comparten contratos, motor de ejecución, procedimientos, diagnósticos, seguridad e integraciones. No se mantienen dos implementaciones de una misma regla de negocio.

## Principios

- Offline-first; la conexión amplía capacidades pero no es requisito para el soporte esencial.
- Free-first AI; ninguna consulta cloud genera gasto sin autorización explícita.
- IA intercambiable y modelo local opcional.
- La IA recomienda; un motor determinístico valida; el técnico autoriza.
- Integrar software maduro antes de reinventarlo.
- Una implementación canónica por capacidad y algoritmo.
- Mínimo privilegio, privacidad por defecto y auditoría.
- Reparaciones reversibles siempre que sea posible.
- Compatibilidad y estado de validación visibles.
- No presentar como estable una función no probada.

## Fuera del alcance inicial

- Crear otro RMM completo.
- Crear otro ITSM completo.
- Crear un clonador de discos o gestor multiboot propio.
- Descargar e instalar controladores genéricos indiscriminadamente.
- Ejecutar comandos arbitrarios generados por IA.
- Administrar drivers genéricos de macOS; se priorizan mecanismos Apple y diagnóstico de extensiones/periféricos de terceros.

## Indicadores de éxito

- Diagnósticos reproducibles y operaciones verificadas.
- Bajas tasas de reversión y errores por procedimiento.
- Tiempo desde solicitud hasta diagnóstico.
- Intervenciones con historial completo.
- Funciones esenciales disponibles sin Internet.
- Compatibilidad probada por SO y arquitectura.
- Integraciones con procedencia, licencia y versión documentadas.
