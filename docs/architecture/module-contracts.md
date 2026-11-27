# Contratos y límites de módulos

**Estado:** contratos ejecutables implementados con JSON Schema 2020-12 y validador Go para manifiestos, acciones, intervenciones, eventos, checkpoints y descriptores de artefactos. El descriptor artifact v1 se valida desde CLI; el resolver comprueba plataforma real, ruta portable, archivo regular, tamaño exacto y SHA-256. ADR-0015 selecciona TUF/go-tuf v2 para autenticar el catálogo; el cliente y la cache offline aún deben implementarse y probarse. Siguen pendientes cargador de módulos y supervisor.

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

El contrato `artifact-descriptor.schema.json` v1 declara `schemaVersion`, `artifactId`, versión SemVer, SO/arquitectura, `relativePath`, SHA-256, `sizeBytes`, licencia y `sourceUri`. Se valida mediante `soptraz validate artifact`. El límite de documentos CLI es 1 MiB; cada binario está limitado a 512 MiB.

`internal/modules/artifact` consume un descriptor confiable y resuelve solo un archivo instalado bajo una raíz administrada. Comprueba SO/arquitectura reales exactos (sin fallback), ruta portable, ausencia de traversal y symlinks/reparse points, archivo regular, tamaño máximo, igualdad exacta entre `sizeBytes` y bytes leídos y SHA-256. No consulta `PATH`, no descarga, no extrae ni ejecuta el archivo.

`sourceUri` es procedencia declarada, no autenticidad. SHA-256 prueba correspondencia entre el archivo y el descriptor, no la identidad del publicador. Sigue pendiente un catálogo autenticado mediante firma del publicador o política administrada de confianza.

La resolución usa un handle `os.Root`, que confina operaciones relativas frente a traversal y enlaces fuera de la raíz. No es sandbox ni impide por sí solo bind mounts, límites de filesystem o todos los archivos especiales; la raíz debe ser privada y escribible solo por SOPTRAZ/usuario confiable. La ruta devuelta es un nombre de punto en el tiempo, no una capacidad ejecutable; el futuro supervisor deberá reabrir/verificar justo antes de crear el proceso.

## Catálogo autenticado y metadata TUF

El catálogo no se autenticará con un SHA-256 aislado ni con una firma propietaria. ADR-0015 adopta go-tuf/v2: raíz fijada en la aplicación; roles root, timestamp, snapshot y targets; umbrales y delegaciones; expiración y versiones monotónicas; consistent snapshots. Cada target TUF vinculará su descriptor y binario mediante longitud y hashes.

No confiar en una raíz descargada mediante Trust-On-First-Use. No usar UpdaterConfig.UnsafeLocalMode para convertir una cache incompleta o vencida en confianza: la librería documenta que se aparta del flujo TUF. Field offline solo aceptará metadata previamente autenticada, persistida atómicamente, no expirada y con estado anti-rollback comprobado. La restauración tras reinicio debe estar cubierta por pruebas.

El Updater de go-tuf no es seguro para concurrencia: SOPTRAZ deberá serializarlo o aislar instancias. El fetcher tendrá timeout finito, límites de bytes y reintentos acotados. La autenticidad del catálogo no equivale a autorización de ejecución.

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
