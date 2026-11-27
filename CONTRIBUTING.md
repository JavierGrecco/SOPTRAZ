# Contribuir a SOPTRAZ

## Principios

- Mantener límites modulares.
- No duplicar reglas de dominio.
- Preferir herramientas maduras con licencia y riesgo aceptables.
- No agregar dependencias sin justificación.
- No habilitar operaciones mutantes sin contrato, riesgo, autorización, auditoría y verificación.
- Mantener documentación y ADR alineadas con el código.

## Flujo

1. Crear rama corta desde main.
2. Vincular el cambio a issue o decisión.
3. Implementar el cambio más pequeño verificable.
4. Ejecutar pruebas de módulo y contrato.
5. Ejecutar gofmt, go vet ./... y go test ./....
6. Actualizar documentación.
7. Abrir PR con alcance, riesgos, evidencias y rollback.
8. Integrar con CI verde y revisión.

## Convenciones

- Código y nombres técnicos en inglés.
- Documentación de producto/operación en español inicialmente.
- Commits convencionales.
- Errores estructurados y accionables.
- No registrar secretos ni datos personales.
- Cada integración incluye licencia, procedencia, versión y hash.
