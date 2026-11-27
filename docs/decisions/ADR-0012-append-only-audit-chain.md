# ADR-0012: Registro append-only con cadena de hashes por intervención

- Estado: Aceptada
- Fecha: 2026-11-27

## Contexto

Las operaciones de soporte deben tener secuencia, actor, hora, resultado y evidencia comprobables. Un historial editable sin protección no permite detectar cambios accidentales ni pérdida parcial de registros.

## Decisión

- El dominio de auditoría define `Event`, `Draft` y la interfaz `audit.Store`.
- Cada intervención mantiene una secuencia propia.
- Cada evento contiene previousHash y hash SHA-256 calculado con un separador de dominio.
- Field persiste los eventos en SQLite; el adaptador bloquea UPDATE y DELETE con triggers.
- La transacción de anexado actualiza el evento y la cabecera de secuencia/hash de forma atómica.
- Verify recalcula toda la cadena y compara con la cabecera.
- Antes de anexar, el store compara la cabecera persistida con el hash de la última fila; no continúa una cadena cuya cabecera ya fue manipulada.
- Los módulos de producto dependen de la interfaz audit.Store, no de SQLite.

## Alternativas descartadas

- Logs de texto no estructurados: no ofrecen esquema, actor/relación ni verificación.
- Una sola fila mutable de estado: pierde el historial de cambios.
- Usar solo hashes por archivo: no expresa orden de eventos ni continuidad de una intervención.
- Desarrollar dos algoritmos distintos para Field y Hub: introduciría divergencia.

## Consecuencias

- Cambios parciales, secuencias con huecos, hashes no coincidentes y truncamientos con cabecera intacta son detectables.
- Los triggers evitan cambios accidentales por SQL convencional. `recursive_triggers=ON` asegura que `INSERT OR REPLACE` dispare el trigger de borrado y no reemplace una fila auditada silenciosamente.
- No es un registro WORM ni resiste a un administrador capaz de reescribir íntegramente el archivo SQLite.
- Un checkpoint firmado y almacenado fuera de la base es necesario para detectar una reescritura total; queda fuera de este hito.
- La exportación Field → Hub debe conservar evento, secuencia, hash y versión de esquema originales.
