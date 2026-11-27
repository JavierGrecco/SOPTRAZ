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

El catálogo no se autenticará con un SHA-256 aislado ni con una firma propietaria. ADR-0015 adopta go-tuf/v2: raíz fijada en la aplicación; roles root, timestamp, snapshot y targets; umbrales y delegaciones; expiración y versiones monotónicas; consistent snapshots. Cada target TUF vinculará su descriptor y binario mediante longitud y hashes. SOPTRAZ exige que `custom` contenga el descriptor artifact v1 completo: el decodificador rechaza claves duplicadas, UTF-8/surrogates inválidos, campos desconocidos y documentos adicionales. Antes de aceptar un artefacto, `MetadataSession.VerifyDescriptor` exige igualdad exacta del descriptor firmado y coincidencia entre la ruta, `sizeBytes`/`length` y SHA-256 del descriptor y del target TUF. La firma TUF cubre también el objeto `custom`; la resolución local vuelve a comprobar los bytes instalados.

No confiar en una raíz descargada mediante Trust-On-First-Use. No usar UpdaterConfig.UnsafeLocalMode para convertir una cache incompleta o vencida en confianza: la librería documenta que se aparta del flujo TUF. Field offline solo aceptará metadata previamente autenticada, persistida atómicamente, no expirada y con estado anti-rollback comprobado. La restauración tras reinicio debe estar cubierta por pruebas.

La primera capa online `TUFClient` envuelve el `updater.Updater` oficial. Su constructor completa un refresh online; usa `GetTargetInfo` para que go-tuf resuelva el árbol de delegaciones y pasa el target por la misma verificación estricta de artifact v1. Serializa las llamadas porque el Updater no es seguro para concurrencia. Como la API oficial solo permite un `Refresh` por instancia, cada refresh explícito crea un Updater nuevo y vuelve a validar la cache frente al root fijado; si falla, invalida el cliente en memoria en vez de servir la instancia anterior. El fetcher tiene timeout HTTP finito, límites de tamaño para metadata, reintentos acotados y no sigue redirects. Solo permite HTTPS; HTTP se habilita explícitamente para loopback en pruebas de laboratorio. `ResolveDescriptor` consulta metadata, pero no llama `DownloadTarget`, no instala ni ejecuta el binario.

Esta primera capa no cierra M1.4: recibe un `TrustAnchor` ya fijado; la aplicación aún no incorpora ni enrola el root de producción, y falta persistir de forma durable/anti-rollback el estado de confianza y probar recuperación offline/reinicio. La metadata cacheada por go-tuf no se considera por sí sola una política offline completa. La autenticidad del catálogo tampoco equivale a autorización de ejecución.

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

## Contrato de acciones de optimización Windows (M1.6)

**Estado:** especificación arquitectónica; todavía no hay acciones mutantes habilitadas.

Además del contrato general de acción, cada tweak Windows declarará:

- `action_id`, versión, categoría, descripción y fuente/version/licencia del código o herramienta.
- Sistemas admitidos: Windows build mínimo/máximo, edición, arquitectura, requisitos de hardware y dependencias.
- Precondiciones detectables, capacidades solicitadas, elevación, riesgo, duración máxima, idempotencia y necesidad de reinicio.
- Estado deseado expresado como DTO tipado; nunca un fragmento de PowerShell, CMD o shell recibido desde la UI.
- Efecto esperado, side effects conocidos y postcondiciones comprobables mediante un lector independiente.
- Política de estado previo y restauración, con alcance por usuario/equipo y clasificación `exact`, `partial` o `unavailable`.
- Requisitos de aprobación y referencia a los eventos/evidencias de auditoría.

### Captura y restauración

La captura se realiza inmediatamente antes del cambio y registra solo los campos necesarios para restaurar esa acción (por ejemplo: existencia de una clave, tipo y valor de registro; tipo de inicio/estado de servicio; estado de tarea; paquete y usuario afectado). El snapshot no se reemplaza por un valor por defecto que provenga de la documentación del tweak.

Cada aplicación registra un identificador de operación y el estado objetivo escrito. Al restaurar:

1. comprobar que el snapshot pertenece a la misma acción, equipo, ámbito y versión compatible;
2. volver a leer el estado actual;
3. restaurar solo si el estado actual coincide con el que dejó aquella operación;
4. ante diferencias atribuibles al usuario, Windows Update, políticas MDM o software externo, detenerse y presentar el conflicto;
5. verificar el resultado de la restauración y guardar el evento.

Esto evita que un botón Undo restaure un valor antiguo sobre una decisión más reciente del usuario. Si no puede detectarse el estado previo con fidelidad, la acción no se etiqueta como reversible exacta. Los cambios de paquetes, componentes, drivers o archivos del sistema requieren mecanismos específicos y no se deben simular con un simple valor de registro.

### Perfiles y ejecución

Un perfil es una lista versionada de `action_id` y parámetros tipados, no un script ni un conjunto de comandos. Antes de aplicarlo, SOPTRAZ genera un plan que muestra acciones elegibles, excluidas, privilegios, reinicios, impactos y restauración disponible. La selección no modifica la configuración hasta recibir aprobación conforme al riesgo.

Las acciones mutantes se bloquean hasta que M1.4 autentique sus artefactos y M1.5 supervise la ejecución sin shell libre, con argumentos allowlisted, entorno mínimo, timeout, cancelación, cuotas de salida y auditoría. Los perfiles recomendados solo contendrán acciones que superaron pruebas de compatibilidad y recuperación.

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
