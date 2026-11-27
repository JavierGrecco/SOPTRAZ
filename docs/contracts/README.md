# Contratos ejecutables de SOPTRAZ

Los contratos externos usan JSON Schema Draft 2020-12. El runtime Go valida los documentos contra esquemas embebidos; no descarga esquemas remotos.

## Tipos iniciales

| Tipo de CLI | Esquema | Finalidad |
|---|---|---|
| manifest | module-manifest.schema.json | Identidad, versión, plataformas, capacidades, permisos, dependencias, licencia e integridad del módulo |
| action | action-definition.schema.json | Definición de una acción, límites de riesgo, privilegios, efectos, verificación y rollback |
| intervention | intervention.schema.json | Registro portable de una intervención técnica |
| event | intervention-event.schema.json | Evento ordenado y correlacionable de la intervención |

Los esquemas se encuentran en `internal/contracts/schemas/`; ejemplos verificables están en `examples/contracts/`.

## Validar desde la CLI

~~~sh
soptraz validate manifest examples/contracts/module-manifest.example.json
soptraz validate action examples/contracts/action-definition.example.json
soptraz validate intervention examples/contracts/intervention.example.json
soptraz validate event examples/contracts/intervention-event.example.json
~~~

El comando es de solo lectura. Devuelve:
- código 0: documento válido;
- código 1: archivo ausente, JSON inválido o contrato no válido;
- código 2: argumentos incorrectos.

El límite inicial es de 1 MiB por documento. El validador solo acepta los tipos incorporados y no resuelve referencias de esquema externas.

## Reglas estructurales iniciales

- Se rechazan propiedades desconocidas en los objetos de primer nivel y objetos anidados cerrados.
- Los módulos `stable` requieren firma Ed25519 además de SHA-256.
- Las acciones de riesgo `high` o `critical` requieren aprobación.
- Las acciones `critical` requieren además privilegios administrativos.
- Las fechas se validan como date-time y los identificadores de intervención/evento como UUID.
- Los contratos declaran compatibilidad de plataforma y arquitectura.

La validación de esquema no sustituye la autorización en tiempo de ejecución, comprobación de firmas, verificación de hash de artefactos ni evaluación semántica del activo. Esas capas se implementarán separadamente.

## Versionado

La versión del contrato (`schemaVersion`) se separa de la versión del módulo/acción. Cambios incompatibles crean una nueva versión de esquema y una ventana de migración. No se cambia un esquema publicado de forma silenciosa.
