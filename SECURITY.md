# Política de seguridad

## Reporte responsable

No publicar detalles explotables sin coordinar una corrección. Usar el mecanismo de reporte privado de GitHub si está habilitado o contactar a los mantenedores por el canal privado que se publique.

## Principios

- Mínimo privilegio.
- No guardar claves API en USB.
- No recopilar contraseñas ni claves de cifrado.
- Sanitizar logs antes de enviarlos a IA.
- No ejecutar comandos libres generados por IA.
- Verificar firma/hash/procedencia de herramientas y actualizaciones.
- Aislar procesos externos.
- Validar parámetros, rutas, dispositivos y volúmenes.
- Operaciones críticas con autorización explícita.
- Auditoría protegida contra alteración accidental.
- Sin telemetría cloud por defecto.

## Pruebas

Las reparaciones se prueban primero en VMs, discos virtuales y laboratorio. No probar borrado, particionado, reparación destructiva, firmware o arranque en equipos de producción.

## Incidentes

Toda vulnerabilidad confirmada se registra, prioriza, corrige, prueba y documenta. Los lanzamientos afectados se identifican por versión y hash.
