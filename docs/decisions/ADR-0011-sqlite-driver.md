# ADR-0011: Driver SQLite sin CGO para SOPTRAZ Field

- Estado: Aceptada para la primera implementación de Field
- Fecha: 2026-11-27

## Contexto

Field debe crear y consultar una base local desde Windows, macOS y Linux, sin requerir que el técnico instale compiladores C ni depender de CGO en cada build. El producto también debe tener builds reproducibles en CI para las tres plataformas.

## Alternativas evaluadas

- `modernc.org/sqlite`: driver `database/sql` transpiled/pure Go, no requiere CGO, BSD-3-Clause, soporta Windows, Darwin y Linux en arquitecturas comunes.
- `github.com/mattn/go-sqlite3`: driver muy utilizado y basado en SQLite nativo, pero requiere CGO y gcc; eso agrega fricción a las builds portables y cross-compilation.
- `github.com/ncruces/go-sqlite3`: opción sin CGO basada en SQLite WebAssembly; se mantiene como alternativa de benchmark si tamaño/arranque/performance justifican reevaluación.

## Decisión

Adoptar `modernc.org/sqlite v1.63.0` para Field, con `modernc.org/libc v1.80.1` fijado explícitamente tal como exige la documentación del proyecto. Limitar el pool de base de datos a una conexión en Field y utilizar transacciones inmediatas para el registro secuencial de auditoría.

## Consecuencias y controles

- Sin dependencia de CGO/gcc en los builds de Field.
- La licencia del driver es BSD-3-Clause; SQLite y componentes de terceros conservan sus avisos y licencias.
- El módulo incluye código SQLite transpiled y una dependencia transitiva extensa; antes de distribuir el binario se deberá generar y revisar el SBOM y conservar los avisos de terceros.
- El mantenedor documenta una penalización de CPU en consultas CPU-bound frente a SQLite compilado en C. El registro de auditoría tiene una carga pequeña y principalmente transaccional, pero se medirá en hardware real.
- La versión de `modernc.org/libc` debe coincidir exactamente con la fijada por el módulo sqlite.
- Se probará build, apertura, cierre, recuperación y persistencia en Windows, macOS ARM64 y Linux antes de declarar estable el modo portable.

## Reevaluación

Medir arranque, tamaño del binario, escritura secuencial, concurrencia, fsync y comportamiento en unidades USB. Si las métricas no cumplen, comparar con ncruces/go-sqlite3 en un benchmark de laboratorio sin modificar el contrato `audit.Store`.

## Referencias

- https://pkg.go.dev/modernc.org/sqlite
- https://github.com/modernc-org/sqlite
- https://github.com/mattn/go-sqlite3
