# Estrategia de integración

**Regla:** integrar software maduro antes de reimplementar sus capacidades. Integrar no significa copiar o redistribuir código.

## Matriz inicial

| Proyecto | Área | Decisión | Límite |
|---|---|---|---|
| GLPI | ITSM, tickets, activos y organizaciones | Conector Hub | No desarrollar otro Service Desk completo |
| Fleet | Endpoint management | Conector Hub opcional | Aprovechar gestión existente |
| osquery | Inventario y consultas | Adaptador diagnóstico | No repara por sí mismo |
| Sysinternals | Diagnóstico Windows | Herramientas opcionales | Respetar licencia y distribución |
| smartmontools | Almacenamiento | Adaptador | SMART |
| Ventoy | Arranque multiboot | Distribución rescate | No desarrollar boot manager |
| SystemRescue | Rescate Live | Distribución rescate | Aprovechar herramientas incluidas |
| Rescuezilla | Imagen/clonación | Distribución rescate | No desarrollar clonador |
| Ollama | IA local | Adaptador AI Gateway | Sin cuota cloud |
| Dell/HP/Lenovo/Intel/AMD/NVIDIA | Drivers/firmware | Adaptadores OEM | Modelo exacto y compatibilidad |
| Action1 | Parches | Conector opcional | Evitar duplicar patch management |
| PDQ | Despliegue software | Conector opcional | Automatización existente |
| MeshCentral | Acceso remoto | Integración futura | No dependencia de Field |
| Sysora Office Kit | Configuración/benchmark | Estudiar/probar | No asumir API, licencia o redistribución |

## Evaluación obligatoria

1. Madurez, actividad, versiones y mantenedor.
2. Licencia de código, binarios, assets y dependencias.
3. Compatibilidad por SO, versión y arquitectura.
4. Seguridad, avisos, firma y procedencia.
5. API/CLI y automatización.
6. Recursos y funcionamiento offline.
7. Frecuencia de mantenimiento.
8. Riesgo de privilegios y efectos secundarios.
9. Prueba en laboratorio.
10. Actualización, rollback y retirada.

## Clasificación

- Adoptar: usar sin cambios si la licencia lo permite.
- Integrar: crear adaptador y contratos.
- Extender: modificar respetando licencia y atribución.
- Conectar: consumir API o abrir herramienta oficial.
- Estudiar: benchmarking sin dependencia.
- Descartar: riesgo, licencia incompatible o bajo valor.

## Licencias

No redistribuir herramientas, ISO, drivers o paquetes sin verificar permiso. Mantener avisos, licencias, atribuciones y SBOM. Descargas desde fuentes oficiales con versión y hash. Verificar firma del proveedor cuando esté disponible.

## Windows drivers

Prioridad OEM por modelo y hardware ID. PnPUtil y SetupAPI.dev.log son fuentes técnicas. Respaldar antes de cambios. No instalar solo por fecha reciente. No limpiar Driver Store automáticamente.

## macOS drivers

No desarrollar descargador genérico. Priorizar Apple Software Update, herramientas Apple y diagnóstico de extensiones/periféricos de terceros.

## POC

Cada integración estratégica requiere ficha/ADR con objetivo, escenario, licencia, versión, seguridad, prueba de laboratorio, resultados y decisión.
