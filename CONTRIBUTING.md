# Contribuir a SOPTRAZ

## Principios

- Mantener límites modulares.
- No duplicar reglas de dominio.
- Preferir herramientas maduras con licencia y riesgo aceptables.
- No agregar dependencias sin justificación.
- No habilitar operaciones mutantes sin contrato, riesgo, autorización, auditoría y verificación.
- Mantener documentación y ADR alineadas con el código.

## Flujo de trabajo y ramas

1. Usar `main` como única rama de larga vida.
2. Crear una rama corta desde el `main` actualizado por cada cambio coherente.
3. Nombrar las ramas con un prefijo claro: `feat/`, `fix/`, `test/`, `docs/` o `chore/`.
4. Evitar ramas paralelas para documentación que pertenece a una funcionalidad: actualizar README, roadmap, arquitectura y ADR en el mismo PR funcional cuando describan ese cambio.
5. No apilar ramas dependientes salvo necesidad técnica explícita; cada PR debe poder revisarse y probarse de forma independiente.
6. Ejecutar pruebas de módulo y contrato; además, verificar `gofmt`, `go vet ./...` y `go test ./...`.
7. Abrir un PR con alcance, riesgos, evidencias, límites y rollback. El CI completo debe pasar sobre el SHA exacto de la cabeza del PR.
8. Integrar con squash cuando sea apropiado. Tras el merge, la rama corta debe eliminarse; no reutilizar ramas fusionadas.
9. Si una rama conserva commits exclusivos que no llegaron a `main`, no eliminarla automáticamente: primero recuperar o documentar ese trabajo.

La automatización de higiene de ramas elimina solo ramas cuyo PR fue fusionado y cuyo SHA de cabeza sigue coincidiendo exactamente, o ramas sin PR que no contienen commits exclusivos frente a `main`. Omite siempre `main`, ramas protegidas y ramas asociadas a PR abiertos. La ejecución manual es dry-run por defecto.

## Convenciones

- Código y nombres técnicos en inglés.
- Documentación de producto/operación en español inicialmente.
- Commits convencionales.
- Errores estructurados y accionables.
- No registrar secretos ni datos personales.
- Cada integración incluye licencia, procedencia, versión y hash.
