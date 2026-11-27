# ADR-0010: Ciclo de vida explícito de intervenciones

- Estado: Aceptada
- Fecha: 2026-11-27

## Contexto

Una intervención no debe saltar directamente de diagnóstico a ejecución. Debe existir una secuencia visible para aprobación, operación, verificación y cierre.

## Decisión

Modelar estados finitos y transiciones explícitas en el dominio Go. La primera implementación incluye:

- created
- triaged
- diagnosed
- awaiting_approval
- approved
- executing
- verifying
- resolved
- failed
- cancelled
- closed

Las transiciones se mantienen en un único mapa canónico del módulo `internal/intervention` y se cubren con pruebas de transiciones permitidas y prohibidas.

## Consecuencias

- No se puede ejecutar una intervención directamente desde created/diagnosed.
- La aprobación queda como estado explícito.
- El cierre es terminal.
- Las fallas pueden volver a triage para una nueva evaluación.
- Los cambios de estado deberán emitir eventos de auditoría cuando se implemente el event store.

## Fuera de alcance de esta ADR

Persistencia, autorización de usuarios, aprobaciones criptográficas, ejecución de acciones y event store completo. Esas capacidades requieren implementación y pruebas separadas.
