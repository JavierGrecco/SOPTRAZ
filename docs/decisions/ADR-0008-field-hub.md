# ADR-0008: Field y Hub con contratos compartidos

- Estado: Aceptada
- Fecha: 2026-11-27

## Decisión

Field es una aplicación local/portable y Hub la composición multiusuario/organizacional. Ambas usan el mismo núcleo, modelos de intervención, APIs de módulos y procedimientos.

## Consecuencias

- No duplicar reglas de negocio.
- Field funciona sin Hub.
- Sincronización explícita, segura, idempotente y reanudable.
- Hub agrega organización y coordinación; no sustituye el motor técnico local.
