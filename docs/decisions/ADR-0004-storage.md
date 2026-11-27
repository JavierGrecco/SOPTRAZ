# ADR-0004: SQLite Field y PostgreSQL Hub

- Estado: Aceptada como dirección
- Fecha: 2026-11-27

## Contexto

Field debe operar sin servidor; Hub requiere acceso multiusuario concurrente.

## Decisión

- SQLite local para Field.
- PostgreSQL para Hub.
- Migraciones versionadas.
- Repositorios/contratos separados del SQL concreto.
- Exportación/importación mediante eventos y paquetes versionados.

## Consecuencias

No compartir SQLite abierto sobre red. WAL requiere procesos en el mismo host y no funciona en filesystem de red. Field/Hub sincroniza por protocolo idempotente, no copiando archivos de base de datos.
