# ADR-0009: JSON Schema Draft 2020-12 para contratos entre módulos

- Estado: Aceptada
- Fecha: 2026-11-27

## Contexto

SOPTRAZ combina Go, futuras capacidades Python, UI TypeScript, adaptadores de sistema operativo y herramientas externas. Los contratos internos del lenguaje no son suficientes para interoperabilidad y validación en CI.

## Decisión

Usar JSON Schema Draft 2020-12 como formato interoperable para manifiestos de módulos, definiciones de acciones, intervenciones y eventos. El runtime Go valida los documentos con `github.com/santhosh-tekuri/jsonschema/v6` fijado a v6.0.3 (Apache-2.0).

Los esquemas de confianza del producto se embeben en el binario. No se descargan esquemas remotos durante la validación. Se habilitan assertions de formato.

## Consecuencias

- Contratos consumibles desde Go, Python, TypeScript y tooling de CI.
- Rechazo de propiedades no definidas donde el esquema cierra objetos.
- Fixtures compartidos entre lenguajes.
- La validación estructural se separa de las reglas semánticas y la autorización.
- Dependencia externa de validación requiere revisión de actualizaciones, licencia y pruebas de conformidad.

## Alternativas

- Struct tags y validación Go propia: insuficiente como contrato multi-lenguaje.
- Validadores propios: riesgo de divergencia con el estándar.
- Protocol Buffers: opción sólida para RPC, pero menos conveniente para manifiestos legibles, catálogos y herramientas operativas JSON.

## Referencias

- https://json-schema.org/draft/2020-12
- https://github.com/santhosh-tekuri/jsonschema
