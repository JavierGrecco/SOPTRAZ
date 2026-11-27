# ADR-0006: Ejecución tipada, segura y auditable

- Estado: Aceptada
- Fecha: 2026-11-27

## Decisión

UI e IA solicitan IDs de acciones registradas con parámetros tipados. El ejecutor:
1. Busca procedimiento registrado/firmado.
2. Valida esquema y parámetros.
3. Evalúa capacidades y permisos.
4. Comprueba riesgo, activo y objetivo.
5. Requiere aprobación según política.
6. Ejecuta con timeout y argumentos separados.
7. Registra salida, código, duración y evidencia.
8. Verifica resultado.
9. Ejecuta recuperación documentada cuando sea posible.

No se ejecutan comandos libres generados por IA ni cadenas interpoladas en shell.
