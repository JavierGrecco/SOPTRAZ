# Contratos ejecutables de SOPTRAZ

Los contratos externos usan JSON Schema Draft 2020-12. El runtime Go valida los documentos contra esquemas embebidos; no descarga esquemas remotos.

## Tipos iniciales

| Tipo de CLI | Esquema | Finalidad |
|---|---|---|
| manifest | module-manifest.schema.json | Identidad, versión, plataformas, capacidades, permisos, dependencias, licencia e integridad del módulo |
| action | action-definition.schema.json | Definición de una acción, límites de riesgo, privilegios, efectos, verificación y rollback |
| intervention | intervention.schema.json | Registro portable de una intervención técnica |
| event | intervention-event.schema.json | Evento ordenado y correlacionable de la intervención |

Los esquemas están en `internal/contracts/schemas/`; ejemplos verificables, en `examples/contracts/`.

## Validar desde la CLI

~~~sh
soptraz validate manifest examples/contracts/module-manifest.example.json
soptraz validate action examples/contracts/action-definition.example.json
soptraz validate intervention examples/contracts/intervention.example.json
soptraz validate event examples/contracts/intervention-event.example.json
~~~

El comando es de solo lectura. Código de salida: 0 documento válido; 1 archivo ausente, JSON inválido o contrato inválido; 2 argumentos incorrectos.

El límite inicial es de 1 MiB por documento. Solo se aceptan tipos incorporados. No se descargan esquemas remotos.

## Reglas estructurales

- Se rechazan propiedades desconocidas en objetos cerrados.
- Los módulos `stable` requieren una firma Ed25519 declarada además de SHA-256.
- Las acciones de riesgo `high` o `critical` requieren aprobación.
- Las acciones `critical` requieren también privilegios administrativos.
- Efectos destructivos (borrar datos, escribir firmware o arranque) exigen riesgo crítico, administrador y aprobación.
- La instalación de drivers exige riesgo alto/crítico, administrador y aprobación.
- Los esquemas de entrada/salida de cada acción se compilan como JSON Schema y no pueden resolver referencias externas.
- Fechas e identificadores se validan como date-time y UUID.

La validación estructural no sustituye la autorización en tiempo de ejecución, la comprobación criptográfica de firmas, la verificación de hashes de artefactos ni la evaluación semántica del activo.

## Versionado

La versión del contrato (`schemaVersion`) se separa de la versión del módulo/acción. Cambios incompatibles requieren una nueva versión de esquema y migración. No se modifica silenciosamente un esquema publicado.
