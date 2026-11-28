# Trabajo activo y guía de continuidad

**Actualizado:** 2026-11-28  
**Estado global:** SOPTRAZ sigue en desarrollo. M1.4 no está aceptado y todavía no existe un catálogo offline de producción.

> **Empezá por este archivo.** El README enlaza aquí para explicar qué se está construyendo, dónde continuar, qué ya está hecho y qué condiciones permiten cerrar cada etapa. Este documento es la única lista operativa de trabajo en curso; los ADR definen las decisiones, los issues conservan el seguimiento y los PR contienen los cambios revisables.

## 1. Cómo orientarse en cinco minutos

1. [README principal](../../README.md): producto, principios y estado resumido.
2. **Este documento:** prioridad actual, ramas activas y secuencia de trabajo.
3. [Contribuir](../../CONTRIBUTING.md): ramas, commits, PR y reglas de integración.
4. [Desarrollo local](../development/local-development.md): Go fijado, configuración en macOS/Windows/Linux y comandos reproducibles.
5. [Estrategia de pruebas](../quality/testing-strategy.md) y [revisión de fallos CI](../quality/ci-failure-review.md): matriz de calidad y causas raíz.

Para M1.4, las decisiones que no deben reinterpretarse en código están en [ADR-0015 TUF](../decisions/ADR-0015-tuf-artifact-catalog.md), [ADR-0017 SQLite](../decisions/ADR-0017-tuf-sqlite-trust-store.md), [ADR-0019 verificador offline](../decisions/ADR-0019-offline-tuf-catalog-verifier.md), [ADR-0020 tiempo confiable](../decisions/ADR-0020-trusted-time-and-clock-recovery.md), [ADR-0021 diagnóstico horario del SO](../decisions/ADR-0021-os-time-diagnostics.md), [persistencia TUF](../architecture/tuf-state-persistence.md) y [contrato de tiempo confiable](../architecture/trusted-time-provider.md).

## 2. Estado real al corte

### Rama estable

- **main:** SHA bddb1a271dcf8e8947f8b501c9d89bc581d224ab.
- Último hito integrado: PR #49, snapshot SQLite consistente para lectura offline, sin autenticar firmas ni habilitar verificación offline. CI post-merge #360: 6/6 jobs exitosos.
- El código de main no debe describirse como capaz de validar metadata offline, devolver VerifiedTarget, descargar artefactos o ejecutarlos.

### Líneas de trabajo abiertas

| Línea | Estado comprobado | Próxima acción |
|---|---|---|
| [PR #50 — replay TUF compartido](https://github.com/JavierGrecco/SOPTRAZ/pull/50) | Abierto, draft; 36 commits y 11 archivos en la revisión actual. Head actual: 7e7ebb7e8f0d8e2fb647025cff7e135d83a237a9. CI #387 terminó 6/6 en el head anterior `385de982ecbef2b0c3f1f18850249db80c54b03c`; CI #388 se detuvo solo por una línea en blanco extra en el nuevo test de rotación root y se corrigió. CI #389 está ejecutándose en el head actual. | Completar revisión de seguridad, cobertura del loader offline y la integración con evidencia temporal; mantener el PR en draft hasta cerrar esos límites. |
| [PR #31 — recuperación de stages y diagnósticos](https://github.com/JavierGrecco/SOPTRAZ/pull/31) | Abierto, draft, mergeable=false. Head 90028e81a6a392d8285f3218ce0854cb9370cfbf; comparación con main: 185 commits adelante y 24 atrás, 51 archivos cambiados. CI #343 terminó exitoso en ese head exacto. | No fusionar en bloque. Auditar y rescatar sus capacidades en PRs pequeños, rebasados sobre main, con tests e integración independientes. |
| **main** | Rama estable de integración. | Es la base de cada nueva rama corta. |

Inventario observado: main, feat/tuf-shared-generation-replay y fix/tuf-crash-stage-recovery. No crear ramas adicionales para tareas que puedan revisarse dentro de la misma unidad coherente.

**Advertencia sobre PR #31:** el CI verde del head no resuelve su divergencia ni reemplaza revisión manual. Su rama contiene cambios relevantes de diagnósticos TXT para fallos importantes, recuperación tras terminación abrupta, locks interproceso TUF, endurecimiento de rutas/permisos SQLite, política/POC de tiempo y cambios de plataforma. Parte de ese contexto se solapa con archivos que ya evolucionaron en main. Cada bloque debe compararse con el estado actual, adaptarse y probarse por separado; no hacer merge masivo ni cherry-pick indiscriminado de los 185 commits.


## 2.1. Evidencia CI reciente de la línea M1.4

| CI | SHA evaluado | Resultado | Lectura correcta |
|---|---|---|---|
| #387 | `385de982ecbef2b0c3f1f18850249db80c54b03c` | 6/6 aprobado | Loader offline y prueba de firma no autorizada pasaron en esta cabeza. |
| #388 | `cf73ca3ba3f1d4cb4490fc3645b4c92c764539f1` | Fallo de formato | Una línea en blanco extra en el nuevo test de rotación root; no llegó a vet/tests. Corregido en el head siguiente. |
| #389 | `7e7ebb7e8f0d8e2fb647025cff7e135d83a237a9` | En curso al corte | Debe confirmar el formato y la prueba de replay root v1→v2. |

No presentar #387 como CI de la cabeza actual: solo #389 puede validar el último test y el SHA actual.

## 3. Objetivo inmediato: terminar M1.4 sin debilitar TUF

M1.4 entrega un catálogo de artefactos autenticado; no un instalador ni un supervisor de procesos. La implementación debe usar go-tuf v2.5.0 como autoridad criptográfica, mantener el modo estricto y no introducir un verificador de firmas propio.

**Avance en PR #50 (todavía pendiente de aceptación):** además de `replayVerifiedGeneration` y `resolveTargetPath`, se añadió `replayOfflineTargetMetadata`. Recibe un `OfflineSnapshot`, un bootstrap root/pin independientes y una referencia temporal explícita; revalida la root-chain desde el bootstrap, timestamp, snapshot, targets y cada pareja delegada `(rol, padre)` con go-tuf. No hace red y no considera `Parents` de SQLite como autoridad. Las pruebas integradas comparan con el updater oficial, cierran el servidor, alteran `Parents`, eliminan o cambian metadata, usan un pin distinto, prueban expiración y una firma delegada no autorizada aun con links superiores re-firmados correctamente. Se añadió además el caso de root rotada v1→v2 desde bootstrap independiente; CI #389 debe validar esta última prueba. Sigue siendo una primitiva interna: no existe todavía `OfflineCatalogVerifier` de producción, no integra la política de tiempo ADR-0020 ni emite `VerifiedTarget`.

### Cola activa, en este orden

1. **Cerrar correctamente la revisión de PR #50.** Revisar `replayVerifiedGeneration`, `resolveTargetPath`, `replayOfflineTargetMetadata`, la captura online y los tests; confirmar CI en el SHA exacto final y revisión manual de seguridad. La primitiva offline ya resuelve un target desde un snapshot SQLite sin red, pero sigue siendo interna y requiere que el llamador aporte un bootstrap pin independiente y una referencia temporal aceptada. No es aún el `OfflineCatalogVerifier` completo, no integra el proveedor ADR-0020 ni emite `VerifiedTarget`. Mantener el PR en draft hasta cerrar esa revisión.

2. **Completar la orquestación y la integración del loader offline.** El helper `replayOfflineTargetMetadata` ya consume el `OfflineSnapshot`, reproduce el bootstrap/root-chain, verifica timestamp → snapshot → targets y autentica cada rol candidato con `UpdateDelegatedTargets` contra el padre alcanzado por `resolveTargetPath`. Falta integrar `Store.LoadOfflineSnapshot` en un verificador de extremo a extremo, fijar el suministro de `TrustedTimeEvidence` a través de ADR-0020 y completar la cobertura con fixtures firmados. No permitir que el llamador provea una hora arbitraria en una API de producción. El resolver reproduce estas reglas de recorrido:
   - coincidencia por paths y path_hash_prefixes, usando las primitivas de go-tuf;
   - orden firmado de roles y recorrido depth-first pre-order;
   - comportamiento de terminating y prioridades sin saltar un rol candidato;
   - detección de ciclos/roles repetidos y límites máximos de delegaciones;
   - rechazo cerrado si falta metadata de un rol candidato necesario;
   - coincidencia del resultado con Updater.GetTargetInfo sobre los mismos fixtures.
   
   **No confiar en VerifiedRoleMetadata.Parents como fuente de autoridad.** Es un dato persistido/hint; el resolver debe reconstruir el camino desde las declaraciones de delegación incluidas en los roles autenticados. Si no puede demostrar el camino completo para una ruta, no devuelve target.

3. **Cubrir delegaciones con pruebas diferenciales.** Fixtures para rutas literales y glob, hash prefixes, solapamiento/prioridad, terminating, target top-level y delegado, rol ausente, firma de padre inválida, rol repetido, ciclos, profundidad máxima, grafo incompleto y varios padres. Comparar los roles y el target elegidos con go-tuf oficial. Mantener las pruebas de red offline deterministas.

4. **Integrar la lectura SQLite ya disponible con el replay estricto.** Una operación offline deberá consumir un único LoadOfflineSnapshot: bootstrap pin/origen, root chain, high-water, generación activa, blobs y relaciones tomados en una sola transacción. Reproducir siempre la root-chain desde el anchor independiente con replayStoredRootChain; el hash local de SQLite no constituye trust anchor. Ninguna relación o blob del store se considera autenticada por estar almacenada.

5. **Completar evidencia temporal y migración del trust store.** Aplicar ADR-0020, persistir evidencia con procedencia en el esquema previsto v3 y no convertir timestamps locales antiguos en evidencia confiable. Una operación usa una sola referencia temporal fija. Si la evidencia no alcanza para demostrar vigencia, devolver TrustedTimeUnavailable; no usar time.Now() implícito, tolerancias arbitrarias, UnsafeSetRefTime ni UnsafeLocalMode. La expiración se evalúa conservadoramente con el extremo superior del intervalo de incertidumbre.

6. **Construir OfflineCatalogVerifier completo.** Cero HTTP/DNS/sockets. Revalidar root → timestamp → snapshot → targets → delegaciones necesarias; resolver el target por su path auténtico; comprobar descriptor artifact v1 y la igualdad exacta de ruta, tamaño, SHA-256, versión, SO, arquitectura, licencia y procedencia. Solo entonces puede producir un VerifiedTarget. No descarga, instala ni ejecuta.

7. **Definir el fallback sin mezclar sesiones.** Si offline falla y hay red, descartar el updater, los roles y cualquier resultado parcial, crear un updater online nuevo y repetir todo el flujo desde el principio. Nunca continuar online sobre metadata/targets parciales obtenidos por la sesión offline. Cualquier modo histórico de metadata vencida, si se crea, será solo auditoría y jamás devolverá VerifiedTarget.

8. **Cerrar resiliencia y recuperación.** Recuperar del PR #31, en cambios pequeños, pruebas de muerte externa del proceso, stages huérfanos, locks entre procesos, disco lleno, fallo de journal/sync, corrupción y reinicio. Separar la inyección VFS/SQLite de las pruebas físicas de ENOSPC/corte eléctrico. Documentar la limitación: restaurar una copia completa antigua del mismo disco puede retroceder el high-water local; sin TPM/contador monotónico o witness externo no se promete anti-rollback fuerte.

9. **Bootstrap y gobernanza de la root de producción.** Definir canal independiente de enrolamiento, pin, threshold, custodios, rotación, revocación y respuesta a compromiso. No generar ni guardar claves privadas de producción en el repositorio. Un root bootstrap de laboratorio no equivale a una política de distribución productiva.

10. **Auditoría final de dependencias y aceptación.** Revisar SBOM, avisos, licencia Apache-2.0 y dependencias transitivas de go-tuf/Sigstore; ejecutar la matriz completa Windows, macOS y Ubuntu, Race Detector, fuzzing y branch hygiene; completar la revisión manual. Solo entonces proponer cerrar M1.4 y habilitar el consumo desde M1.5. El catálogo nunca ejecutará por sí mismo un binario.

## 4. Criterio objetivo para retirar una tarea de la cola

La cola anterior contiene únicamente trabajo pendiente. Una tarea se retira de la cola activa solo cuando:

1. el comportamiento queda implementado y documentado en la misma unidad de cambio;
2. hay pruebas positivas, negativas, límites y regresiones relevantes;
3. el paquete afectado y la suite completa pasan localmente cuando el entorno lo permite;
4. CI pasa en el SHA exacto final del PR: Windows, macOS, Ubuntu, Race Detector, fuzzing e higiene de ramas;
5. la revisión de seguridad no deja un bypass ni una limitación sin declarar;
6. el PR se integra a main.

Al retirarla, agregar un registro de una línea en “Hitos integrados” con fecha, PR, commit y enlace al CI. No dejar tareas terminadas mezcladas con las pendientes ni borrar la evidencia histórica. Si una prueba falla, se conserva en la cola con causa raíz y evidencia; no se marca completada por haber aplicado un cambio.

## 5. Hitos integrados que ya no deben repetirse

- **Cliente online oficial y resolución delegada:** PR #8. TUFClient usa go-tuf v2.5.0, descriptor artifact v1, fetcher acotado y serialización dentro del proceso; no descarga ni ejecuta binarios.
- **Root trust store:** PRs #22 y #23. Root bootstrap/origen, cadena consecutiva verificada y persistencia; las rotaciones aceptadas sobreviven al reinicio.
- **Generaciones y high-water:** PR #25. SQLite v2, metadata inmutable por rol, manifiesto, relaciones, versiones y generación activa.
- **Captura y commit de metadata:** PR #27. Bytes de caché comparados con el updater oficial, replay con trustedmetadata y activación transaccional.
- **Staging online aislado:** PR #29. Cada refresh usa workspace privado, revalida online y descarta stages ante errores ordinarios; esto no cubre por sí solo muerte abrupta ni modo offline.
- **Lectura offline consistente:** PR #49, commit bddb1a271dcf8e8947f8b501c9d89bc581d224ab. LoadOfflineSnapshot agrupa root chain, high-water, generación y blobs en una lectura SQLite consistente; es lectura, no autenticación ni autorización offline.
- **Replay criptográfico compartido:** PR #50 está en revisión, no integrado. No moverlo a esta lista hasta que se integre.

## 6. Cómo continuar desde un clon limpio

~~~bash
git clone https://github.com/JavierGrecco/SOPTRAZ.git
cd SOPTRAZ
git switch main
git pull --ff-only
git status --short
~~~

Usar la versión Go fijada en go.mod (al corte: **1.27.4**) y seguir [Desarrollo local](../development/local-development.md). Para una nueva tarea, crear rama corta desde el main actualizado; no usar como base la rama divergente del PR #31.

Comandos habituales del core:

~~~bash
export GOTOOLCHAIN=local
go mod download
go mod verify
go mod tidy -diff
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -race ./...
~~~

Para continuar la parte TUF, empezar por:

- internal/modules/catalog/tufgeneration_replay.go
- internal/modules/catalog/tufgeneration.go
- internal/storage/sqlitetuf/generation.go
- internal/storage/sqlitetuf/offline_snapshot.go (si se necesita revisar la API de lectura integrada en PR #49)
- las pruebas de fixtures del paquete internal/modules/catalog.

No es requisito disponer de gh; revisar PR/CI desde GitHub web o la integración GitHub. No cambiar dependencias ni la versión de Go sin una decisión documentada.

## 7. Límite de seguridad que debe conservar cada desarrollador

- El repositorio SQLite y su caché son datos no confiables hasta el replay criptográfico.
- Root final solo es confiable después de reproducir la cadena desde el pin bootstrap independiente.
- El campo Parents guardado no reemplaza las declaraciones firmadas de delegación.
- Una generación completa no implica que incluya todos los roles del repositorio.
- Metadata expirada o tiempo no confiable significa no emitir VerifiedTarget.
- Un target verificado no es permiso para ejecutarlo; M1.5 y su supervisor de procesos son una puerta distinta.
- Una copia antigua completa del disco puede revertir estado local; se debe declarar el nivel de protección real.

## 8. Referencias externas y decisiones ya investigadas

La investigación TUF/offline, Sigstore strict-offline, discusiones python-tuf, Uptane y los análisis formales de rollback/freeze quedaron registrados en [Issue #5](https://github.com/JavierGrecco/SOPTRAZ/issues/5) y en el comentario de investigación de [PR #50](https://github.com/JavierGrecco/SOPTRAZ/pull/50#issuecomment-7243257394). Consultar esas fuentes antes de reabrir la discusión de expiración/offline; no reemplazan las ADR ni las pruebas del repositorio.

---

**Regla de mantenimiento:** cada vez que se integre un hito, actualizar este archivo y el resumen del README en el mismo PR. Sacar la tarea completada de la cola activa, añadirla a “Hitos integrados” y dejar una referencia objetiva. Cuando la cola quede vacía, M1.4 solo se cierra después de verificar todos sus criterios de aceptación y la revisión final de seguridad.
