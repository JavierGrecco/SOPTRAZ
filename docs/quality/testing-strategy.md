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

## Auditoría

- Verificar secuencia, actor, previousHash, hash y cabecera final.
- Rechazar cambios y borrados SQL ordinarios de eventos.
- Probar detección de alteración del JSON, del hash indexado y de truncamiento.
- Rechazar claves JSON duplicadas en todos los niveles y bytes UTF-8 inválidos antes de canonicalizar; probar idempotencia del normalizador con fuzz seeds.
- Probar concurrencia de escritores y persistencia tras cerrar/reabrir la base.
- No confundir hash chain local con almacenamiento WORM; checkpoints externos firmados se probarán por separado.

## Analítica Python

- Fixtures JSON compartidos.
- Pruebas determinísticas y tolerancias justificadas.
- Datos límite/incompletos.
- Golden datasets no sensibles.
- Una sola implementación por modelo.
- Comparación con baselines.

## CI

Compilar/probar Go en runners Windows, macOS y Linux; ejecutar go vet, Race Detector y fuzzing acotado del normalizador JSON en Linux. La suite ordinaria ejecuta siempre los seeds de fuzz; el job dedicado agrega una corrida de 10 segundos sin caché. Ampliar los jobs al incorporar UI, Python y empaquetado.

## Auditoría de CI

El inventario de ejecuciones fallidas, sus causas raíz y las evidencias externas consultadas se mantiene en [Revisión de fallos de CI](ci-failure-review.md). CI debe ejecutar `go mod tidy -diff` y `go mod verify` además del formato, vet, pruebas multiplataforma y Race Detector.
