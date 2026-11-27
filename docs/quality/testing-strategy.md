# Estrategia de calidad y pruebas

## Principios

- Una rutina no termina porque el comando devuelve código cero.
- Cambios pequeños se prueban antes de integrar.
- Cada módulo tiene pruebas unitarias y de contrato.
- Reparaciones primero en VMs/laboratorio.
- Compatibilidad declarada por SO, versión y arquitectura.
- Toda falla genera evidencia reproducible.

## Niveles

1. Formato y análisis estático.
2. Pruebas unitarias.
3. Pruebas de contratos.
4. Integración con simuladores/fixtures.
5. Flujo completo de intervención.
6. Seguridad: permisos, argumentos, paths, secretos e integridad.
7. Compatibilidad por plataforma.
8. Resiliencia: timeout, cancelación, pérdida de red, cuota y reintento.
9. Offline.
10. Recuperación tras interrupción/rollback.

## Operaciones peligrosas

No ejecutar automáticamente en runners operaciones de borrado, particionado, reparación destructiva, firmware o cambios de arranque. Usar VMs, discos virtuales y simuladores.

Cada procedimiento mutante requiere detección de objetivo, confirmación de dispositivo/volumen, fixture de respaldo, resultado esperado, prueba de recuperación y auditoría.

## Diagnóstico/IA

- Fixtures de incidentes conocidos.
- Evidencia citada por hallazgo.
- Hechos separados de hipótesis.
- Medición de falsos positivos/negativos.
- Output JSON validado.
- Recomendaciones limitadas a acciones registradas.
- Pruebas de prompt injection en logs y salidas.
- IA nunca amplía permisos.

## Analítica Python

- Fixtures JSON compartidos.
- Pruebas determinísticas y tolerancias justificadas.
- Datos límite/incompletos.
- Golden datasets no sensibles.
- Una sola implementación por modelo.
- Comparación con baselines.

## CI inicial

Compilar/probar CLI Go en runners Windows, macOS y Linux; ejecutar go vet y race detector en Linux. Ampliar al incorporar UI, Python y empaquetado.
