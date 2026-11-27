# Contratos y límites de módulos

**Estado:** contratos ejecutables implementados con JSON Schema 2020-12 y validador Go para manifiestos, acciones, intervenciones, eventos y checkpoints. Existe una primera pieza del registro: un resolver de artefactos instalados con coincidencia exacta de plataforma, hash y política de rutas. Aún no existe catálogo autenticado, cargador de módulos ni supervisor para ejecutar herramientas externas.

## Estructura de un módulo

Cada módulo contiene responsabilidad/límites, interfaz pública, casos de uso, adaptadores, esquemas, configuración, pruebas, documentación y manifiesto.

Un módulo no expone sus tablas ni tipos internos. Los consumidores usan DTO/contratos públicos.

## Manifiesto mínimo

- module_id: ID estable, por ejemplo soptraz.storage.
- module_version: SemVer.
- contract_version.
- description.
- platforms: SO, versión y arquitecturas.
- capabilities: acciones expuestas.
- permissions: privilegios solicitables.
- risk_classes.
- dependencies: herramientas y módulos requeridos.
- integrity: SHA-256 y firma del artefacto con Ed25519 o ECDSA P-256; el `keyId` se deriva del material público canónico.
- license.
- support_status: experimental, preview, stable o deprecated.

## Resolución de artefactos instalados

`internal/modules/artifact` recibe un descriptor confiable desde un futuro catálogo autenticado y resuelve solo un archivo ya instalado bajo una raíz administrada. Comprueba SO/arquitectura exactos (sin fallback), ruta relativa portable, ausencia de traversal y enlaces simbólicos, archivo regular, tamaño máximo y SHA-256. No consulta `PATH`, no descarga, no extrae y no ejecuta el archivo.

El `SourceURI` y el SHA-256 del descriptor no autentican por sí solos al editor. El catálogo debe obtener ese descriptor de una fuente firmada o de una política administrada de confianza. El resolver solo demuestra que los bytes encontrados coinciden con el digest declarado. La ruta devuelta está canonicalizada; el futuro supervisor debe volver a verificar justo antes de crear el proceso y operar desde un directorio protegido contra escritura no autorizada.

## Contrato de acción

Cada acción declara ID estable, descripción, entrada JSON Schema, salida estructurada, capacidades, privilegios, riesgo, timeout, idempotencia, interrupciones, evidencias, verificación, rollback y requisito de aprobación.

Ejemplo ilustrativo, no ejecutable:

~~~json
{
  "action_id": "windows.driver.export",
  "contract_version": "1.0",
  "risk": "low",
  "requires_admin": true,
  "idempotent": true,
  "requires_approval": false,
  "verification": "exported_packages_integrity"
}
~~~

## Comunicación

- Interfaces Go para módulos en el mismo proceso.
- JSON Schema para workers/adaptadores externos.
- OpenAPI para API Hub.
- Eventos para hechos confirmados.
- Correlation ID e intervention ID en cada flujo.

## Seguridad

La UI y la IA deberán solicitar IDs registrados y argumentos tipados, nunca comandos libres. La ejecución de módulos externos queda bloqueada hasta implementar un supervisor con ejecutable resuelto desde el manifiesto, verificación de hash, allowlist de argumentos/capacidades, timeout, cancelación, límites de stdout/stderr y auditoría. La existencia del esquema no implica que el módulo ya pueda ejecutarse. Operaciones críticas identifican explícitamente el dispositivo/volumen y requieren aprobación.

## Versionado

Cambios compatibles incrementan minor; incompatibles incrementan major. Acciones se deprecian antes de retirarse. Fixtures compartidos prueban contratos. Campos desconocidos no pueden alterar privilegios ni riesgo.

## Integraciones externas

Se ejecutan mediante adaptadores aislados, con lista de comandos/capacidades permitidas y ciclo de actualización independiente. El supervisor aún es una dependencia pendiente; no se ejecuta código de terceros directamente desde la UI ni desde un manifiesto. El resolver de artefactos no equivale a un launcher ni a un sandbox.
