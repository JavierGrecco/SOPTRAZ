# ADR-0017: Almacén durable de confianza TUF en SQLite

- Estado: Aceptada. La root-chain SQLite y su integración con `NewPersistentTUFClient` están integradas en `main` (PR #22 y #23). PR #25 integra el esquema de generaciones y la API de commit/carga atómica con high-water por rol; falta conectar esa API al staging y al flujo del updater. Crash/restart abrupto, bootstrap de producción y modo offline siguen pendientes.
- Fecha: 2026-11-28
- Relacionada: ADR-0011, ADR-0015, Issue #5 (M1.4), Issue #4 (M1.5).
- Alcance: estado de confianza, metadata TUF autenticada, high-water marks y generaciones del catálogo. No incluye targets ejecutables.

## Contexto

La primera capa online TUF ya utiliza el `updater.Updater` oficial de go-tuf v2.5.0 y resuelve roles delegados. PR #20 corrigió la recreación del updater para que un root que ya fue verificado se conserve entre refrescos dentro del mismo proceso, incluso si después falla timestamp/snapshot/targets.

Ese arreglo no era persistencia durable: `TUFClient.config.LocalTrustedRoot` vivía en memoria. PR #23 añade `NewPersistentTUFClient`, que conserva la cadena de roots en SQLite y la reproduce desde el pin bootstrap antes de inicializar el updater. PR #25 añade `CommitGeneration` y `LoadActiveGeneration`: el almacén puede comprometer los bytes exactos de timestamp/snapshot/targets y las delegaciones suministradas por el caller, sus relaciones, high-water y `active_generation` dentro de una transacción. Esta API todavía no está integrada con `TUFClient` ni crea generaciones a partir del cache del updater; el updater oficial sigue conservando metadata individual en su caché hasta que implementemos el staging y la importación transaccional.

SOPTRAZ ya usa `modernc.org/sqlite` sin CGO en Field. El adaptador de auditoría aporta convenciones que podemos reutilizar: una conexión, transacciones inmediatas, `busy_timeout`, `foreign_keys`, `synchronous=FULL`, migraciones versionadas y tests de recuperación. El estado del catálogo debe permanecer separado de `audit.sqlite`: sus ciclos de vida, políticas de backup y superficies de acceso no son los mismos.

## Alternativas

1. **Confiar solo en el directorio cacheado de go-tuf.** Se descarta como almacén de confianza completo: cada archivo se persiste individualmente y no se actualizan atómicamente metadata, high-water y generación activa. La caché sigue siendo útil como staging temporal del updater oficial.
2. **Generaciones inmutables en filesystem con un puntero activo.** Es viable, pero obliga a definir y probar renames durables, reemplazo de puntero, permisos, limpieza y recuperación diferentes en Windows, macOS y Linux. No aporta una ventaja suficiente frente a la transacción SQLite ya disponible.
3. **Un SQLite separado con blobs de metadata y commits transaccionales.** Se selecciona: reutiliza el driver portable sin CGO y permite que metadata, high-water marks, historial de raíces y generación activa cambien en transacciones verificables.
4. **Una única base compartida con auditoría.** Se descarta: aumenta el acoplamiento y permitiría que importación, retención o recuperación del ledger modifique accidentalmente el dominio de confianza del catálogo.

## Decisión

SOPTRAZ implementará un almacén separado, inicialmente `catalog-trust.sqlite`, mediante `modernc.org/sqlite`. El almacén es el registro durable de estado, no un sustituto de la verificación criptográfica de go-tuf.

### 1. Límites y responsabilidades

- La capa `catalog` conserva el protocolo TUF, el trust anchor y la verificación de firmas, delegaciones, expiración, hashes y versiones.
- El paquete de persistencia guarda únicamente registros que la capa `catalog` ya verificó. No interpreta una firma ausente ni convierte una metadata no verificada en confiable.
- El almacén no descarga, instala ni ejecuta targets.
- No comparte tablas con `sqliteaudit`, no se incluye en paquetes de intervención Field → Hub y no se sincroniza con Hub por defecto.
- Las claves privadas TUF nunca se almacenan en esta base. El root y demás metadata son públicos, pero sus versiones y bytes tienen valor de seguridad.

### 2. Modelo de datos

El esquema versionado incluirá al menos:

- **repositories:** identificador estable, origen configurado sin credenciales, hash/versión del trust anchor de enrolamiento, última raíz verificada, generación activa y estado del catálogo.
- **root_chain:** cada root posterior al ancla inicial, con versión, SHA-256 exacto, bytes originales y relación con la raíz anterior. Los roots intermedios no se eliminan: son necesarios para revalidar la secuencia consecutiva desde el pin independiente.
- **generations:** identificador monotónico local, generación padre, root version/hash usado, timestamp de creación, cantidad de roles, digest del manifiesto y estado.
- **role_metadata:** bytes originales de timestamp, snapshot, targets y cada rol delegado cargado; rol padre, versión TUF, expiración, longitud y SHA-256.
- **role_high_water:** última versión/hash/expiración aceptada por repositorio y rol. Una metadata delegada ausente en una generación nueva no reduce su high-water.
- **active_generation:** referencia lógica a la última generación completa comprometida. Se actualiza en la misma transacción que los blobs, el manifiesto y los high-water marks.

El esquema debe imponer claves primarias/foráneas, unicidad por repositorio/generación/rol, límites de versión y tamaños, y prohibir modificaciones de generaciones ya comprometidas salvo por una migración explícita del esquema.

### 3. Dos commits con semánticas diferentes

La rotación de root y la publicación de una generación completa no son la misma operación.

**Commit de cadena de root:**
- El updater puede haber verificado root N+1 y, después, fallar al descargar timestamp.
- SOPTRAZ debe conservar root N+1 para el siguiente intento aunque no exista una nueva generación completa del catálogo.
- Antes de guardar, la capa `catalog` revalida desde el ancla independiente cada root consecutivo candidato con `TrustedMetadata.UpdateRoot`; una raíz ausente, repetida con bytes distintos, fuera de orden o no autorizada se rechaza.
- En una única transacción se agregan todos los roots verificados que faltan y se actualiza latest-root version/hash mediante compare-and-swap frente al estado previo.
- Si esa transacción falla, el proceso no promueve la raíz en memoria ni la presenta como durable.

**Commit de generación:**
- El updater trabaja sobre un staging exclusivo, materializado desde la última generación comprometida.
- Se ejecuta refresh online y se resuelven explícitamente los targets/roles delegados que la aplicación va a publicar.
- Solo después de verificar el conjunto completo se prepara el manifiesto con el root utilizado y versiones/hashes/expiraciones de todos los roles.
- Una transacción inserta la generación, sus blobs y los high-water marks; comprueba que ninguna versión retroceda ni repita número con bytes diferentes; finalmente actualiza `active_generation`.
- Si cualquier comprobación o escritura falla, la transacción revierte completa y la generación activa anterior permanece intacta.

Si una raíz avanzó pero el refresh posterior falló, el root chain sí puede quedar comprometido, mientras la generación activa anterior permanece guardada pero **no es elegible para uso offline** si fue creada bajo una raíz anterior al latest-root. El siguiente refresh debe producir una generación coherente bajo la raíz actual.

### 4. Recuperación después de reiniciar

1. Cargar el trust anchor fijado/enrolado desde la distribución verificada de SOPTRAZ.
2. Leer la cadena de roots persistida y comprobar que su primer ancla coincide exactamente con el pin independiente.
3. Reproducir roots en orden estrictamente consecutivo con las primitivas oficiales de `trustedmetadata`; no confiar en el último `root.json` aislado.
4. Comprobar que versión/hash final coinciden con latest-root del almacén.
5. Materializar la generación activa en un staging temporal con nombres de roles normalizados; comprobar digest, longitud, relaciones y high-water antes de abrir el updater.
6. Crear un updater oficial desde la raíz verificada y el staging. Ningún archivo cacheado se promueve solo por estar presente.
7. Si la cadena, generación o high-water están ausentes, corruptos, inconsistentes o por debajo del estado registrado, bloquear el catálogo y exigir recuperación online o reenrolamiento; no rebajar versiones ni elegir automáticamente un backup viejo.

Un cambio de ancla de enrolamiento tras una actualización de SOPTRAZ requiere una migración explícita que demuestre continuidad con una raíz ya presente en la cadena verificada. Un pin distinto no se acepta silenciosamente ni se reemplaza por el root cacheado.

### 5. Política SQLite

- Archivo separado bajo el directorio local de datos de SOPTRAZ; directorio con permisos mínimos y control de ACL nativo en Windows.
- Driver `modernc.org/sqlite`, sin CGO.
- Una conexión de escritura por store y transacciones `BEGIN IMMEDIATE` para serializar commits.
- `PRAGMA foreign_keys=ON`, `busy_timeout` acotado, `synchronous=FULL`, `trusted_schema=OFF` y journaling rollback `DELETE` explícito inicialmente. No se introduce WAL hasta medir y probar su recuperación y sidecars en las tres plataformas.
- No combinar transacciones SQLite con un puntero de filesystem suponiendo que existe una transacción distribuida.
- Tamaño máximo por metadata, total de blobs por generación, cantidad de roles y número de generaciones retenidas.
- La limpieza elimina generaciones antiguas solo cuando no son activas ni necesarias para auditoría; no borra la cadena histórica de roots que conecta el ancla al root actual.
- Backup no equivale a confianza. Restaurar un backup no puede reducir high-water frente a un ancla hardware/externa cuando exista.

### 6. Invariantes de anti-rollback

- Cada root agregado es consecutivo y queda vinculado por hash a la cadena validada desde el ancla.
- Por rol, versión nueva debe ser mayor que high-water; versión igual exige hash idéntico; menor o mismo número con bytes distintos se rechaza.
- High-water no se elimina porque un rol no aparezca en una generación posterior.
- `active_generation` solo apunta a una generación cuyos blobs y manifiesto están íntegros y cuyos high-water fueron confirmados en la misma transacción.
- La generación activa solo puede ser elegible offline si su root version/hash coincide con el latest-root actual y toda metadata requerida está presente, vigente y validable.
- Si hay un root más nuevo comprometido pero todavía no una generación completa compatible, el catálogo offline queda bloqueado.
- El nivel local detecta retroceso mientras este almacén vigente permanezca intacto. Restaurar una imagen completa antigua del mismo disco puede restaurar también DB y high-water; sin TPM/contador monotónico o witness externo no se afirma anti-rollback fuerte.

### 7. Alcance offline

Esta ADR no habilita el modo offline. El almacén hace posible implementar un verificador offline explícito, pero solo se habilitará cuando este revalide root chain, thresholds, timestamp, snapshot, targets y delegaciones, expiración, hashes/longitudes y high-water. No se usa `UpdaterConfig.UnsafeLocalMode` como bypass. Metadata vencida o incompleta no se sirve por conveniencia.

## Consecuencias

- La autenticidad sigue en go-tuf; SQLite aporta persistencia transaccional y high-water, no criptografía propietaria.
- Root chain y generación activa tienen commits distintos porque una rotación verificada debe sobrevivir a fallos posteriores de timestamp/snapshot/targets.
- El almacenamiento de confianza no depende de la tabla de auditoría ni del ciclo de importación/sincronización de Field.
- Se introduce una migración de esquema y un proceso de recuperación explícito.
- El estado local no protege contra la restauración integral del mismo disco sin un ancla fuera de ese disco.

## Pruebas de aceptación obligatorias

- Root chain desde trust anchor inicial, replay tras reinicio y rechazo de huecos, repetición, bytes distintos, firma inválida y threshold insuficiente.
- Root N+1 verificado seguido por fallo de timestamp: root N+1 permanece comprometido, active generation no avanza y offline queda bloqueado.
- Commits de root chain y generación: kill del proceso antes/durante/después de COMMIT deja el estado anterior completo o el nuevo completo, nunca mezclado.
- High-water: menor rechazado, igual/hash igual idempotente, igual/hash distinto rechazado, mayor aceptado.
- Restaurar una generación vieja mientras high-water vigente permanece debe fallar.
- Restaurar conjuntamente DB + high-water antiguo debe demostrar el límite del nivel local; no debe reportarse como protección fuerte.
- Cache/DB corrupta, WAL/rollback journal residual, disco lleno, permisos insuficientes, schema futuro y dos procesos concurrentes fallan de forma cerrada.
- Test de restart real en subproceso y matriz Windows, macOS, Ubuntu; Race Detector y fuzzing de manifest/role paths.

## Orden de implementación

1. [x] Implementar el store SQLite aislado para root-chain con migración, ancla/origen fijados, append-only, CAS y tests transaccionales (PR #22).
2. [x] Añadir replay de root-chain desde el ancla fijada con `trustedmetadata.UpdateRoot` y comprobar el high-water durable (PR #23).
3. [x] Capturar respuestas versionadas de root, reproducirlas con go-tuf y persistir solo cuando el updater oficial confirma la misma versión (PR #23).
4. [x] Implementar el esquema de generaciones SQLite v2, migración sin pérdida de root-chain v1, metadata por rol/delegación, manifiesto SHA-256 y high-water con procedencia inmutable (PR #25).
5. [x] Implementar `CommitGeneration`/`LoadActiveGeneration` con root binding, CAS, guardas de rollback/equivocation, commit atómico y validación de integridad de metadata y evidencia histórica (PR #25).
6. [ ] Integrar el updater online con staging: obtener los bytes exactos autenticados por go-tuf, resolver los roles delegados que la aplicación necesita y solo entonces llamar a `CommitGeneration`; no declarar completo un grafo que no se haya recorrido.
7. [ ] Añadir tests de terminación abrupta real, crash/restart, corrupción, rollback, disco lleno y concurrencia entre procesos en Windows/macOS/Linux.
8. [ ] Implementar el validador offline explícito y su reporte de assurance.
9. [ ] Completar SBOM, avisos/licencias y política de actualización de go-tuf/Sigstore.
La implementación no se considera production-ready hasta que todas las pruebas de recuperación y anti-rollback estén verdes y el procedimiento de bootstrap/custodia de root esté aprobado.

## Primera implementación del almacén de root-chain (PR #22, 2026-11-28)

Se implementó una primera capa ejecutable, todavía aislada de `TUFClient`:

- Paquete: `internal/storage/sqlitetuf`, con `modernc.org/sqlite` y una base separada de `audit.sqlite`.
- Registro estable del repositorio ligado a su URL y al hash/versión del trust anchor provisionado de manera independiente. Un cambio de origen o anchor no se acepta como re-enrolamiento implícito.
- Cadena append-only de roots, con bytes originales, SHA-256, versión consecutiva y hash de la raíz anterior.
- Commit por lotes en una transacción SQLite, con compare-and-swap del estado esperado. Un lote con un salto o un enlace incorrecto revierte completo.
- Lectura de cadena y high-water desde una única transacción de lectura; vuelve a comprobar tamaño, hash, continuidad y enlace final.
- Configuración SQLite inicial: una conexión, foreign keys, busy timeout, `synchronous=FULL`, `trusted_schema=OFF` y journaling rollback `DELETE`.
- Pruebas de re-apertura del archivo, continuidad, commit por lotes, rollback de lote inválido, CAS, cambio de anchor y detección de alteración de blobs.

El commit de código `c785aebe9190eac870cd3081689f4f794e50f93d` pasó los cinco jobs de CI en [run 46091958671](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46091958671): Windows, macOS, Ubuntu, Race Detector y fuzzing. La documentación de este PR no cambia el alcance del código.

### Límites que permanecen

- El almacén no valida firmas TUF. Recibe únicamente raíces que la capa `catalog` debe haber verificado con `trustedmetadata.UpdateRoot`; al reabrir, el llamador aún debe reproducir toda la cadena desde el anchor independiente.
- Todavía no hay integración que persista automáticamente una root aceptada por `TUFClient`, especialmente cuando el refresh falla después de rotar la root.
- No se implementaron staging de metadata, generaciones, high-water por rol/delegación, ni actualización atómica de `active_generation`.
- Las pruebas actuales incluyen cierre/reapertura ordenados y rollback transaccional ante candidatos inválidos; todavía faltan terminación abrupta real, recuperación crash/restart, dos procesos concurrentes y fallos de disco/permisos en la matriz.
- No habilita catálogo offline ni demuestra anti-rollback frente a restauración integral del mismo disco.

El siguiente paso es implementar staging y commits atómicos de generaciones completas, metadata por rol y high-water persistentes. M1.4 continúa abierto.

## Integración de root-chain con TUFClient (PR #23, integrado)

La implementación de `NewPersistentTUFClient` conecta el almacén anterior con el updater oficial sin sustituir su verificación:

- `TrustAnchor` conserva por separado el root bootstrap original y el root vigente en memoria. El replay siempre comienza desde el bytes exactos y el pin inicial, no desde el último root mutable.
- Al construir el cliente, registra el ID/origen/pin del repositorio, carga la cadena SQLite y reproduce cada root consecutiva con `trustedmetadata.UpdateRoot`. El high-water y el último hash deben coincidir antes de hacer cualquier solicitud de red.
- Un transporte acotado observa solo respuestas `N.root.json` exitosas del mismo origen de metadata y devuelve exactamente los mismos bytes al fetcher oficial de go-tuf.
- Después del refresh, la capa catalog reproduce los candidatos con go-tuf. Antes del commit durable comprueba que la versión final coincide con la versión efectivamente aceptada por el updater oficial. Solo entonces realiza el commit CAS de los roots válidos.
- Si timestamp/snapshot/targets falla después de una rotación root válida, la rotación ya aceptada se conserva en SQLite; la instancia del updater se invalida y el siguiente proceso revalida la cadena desde el pin bootstrap.
- La prueba de reinicio utiliza una segunda instancia, el anchor original y una caché metadata vacía: comprueba que no vuelve a descargar root v2 y continúa desde v2.

PR #23 se integró en `main` como `1988db9`. La matriz definitiva previa al merge pasó 5/5 en [run 46093570302](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46093570302) para el commit `c75ec36e8891bec608d276883c70b04b4c321229`: Windows, macOS, Ubuntu, Race Detector y fuzzing. El CI post-merge se ejecuta sobre el commit de `main`.

### Límites que permanecen

- Solo se persiste root-chain. Timestamp, snapshot, targets y roles delegados no tienen todavía almacenamiento transaccional por generación ni high-water durable por rol.
- La prueba demuestra recuperación con una nueva instancia y una caché vacía después de un refresh que falla en timestamp; todavía faltan pruebas de terminación abrupta del proceso, disco lleno, dos procesos escritores y recuperación de commits interrumpidos.
- `NewPersistentTUFClient` recibe un `*sqlitetuf.Store` ya abierto; el propietario debe mantenerlo abierto durante toda la vida del cliente.
- No se habilita `UnsafeLocalMode`, catálogo offline ni anti-rollback fuerte frente a restauración integral del disco. El bootstrap de producción y la custodia/rotación real de claves siguen abiertos.

El siguiente hito es staging y commit atómico de generaciones completas con metadata por rol, high-water persistentes y `active_generation`. M1.4 no se cierra hasta completar recuperación, políticas de bootstrap y validación offline.
## Contrato de generaciones SQLite v2 (PR #25, integrado)

PR #25 integra la primera implementación ejecutable del modelo de generaciones, todavía como capa de almacenamiento independiente del updater:

- La migración v1→v2 crea `generations`, `generation_metadata`, `generation_role_parents`, `role_high_water` y `active_generations` sin modificar ni eliminar la root-chain existente. La prueba de migración incluye una rotación root v2 preexistente y verifica que sus bytes, hash y enlace con el ancla se conservan.
- `CommitGeneration` requiere `timestamp`, `snapshot` y `targets`; acepta además roles delegados ya resueltos y verificados por `catalog`, junto con sus relaciones padre/hijo. Rechaza roles duplicados, relaciones ausentes, ciclos, tamaños excesivos y nombres no normalizados.
- La operación exige que la root propuesta coincida con latest-root y aplica CAS sobre la generación activa. En una sola transacción inserta la generación, los blobs y relaciones, actualiza high-water por rol y cambia `active_generations`.
- El high-water conserva la versión, hash, expiración y `source_generation_id`; sus claves foráneas lo vinculan a los bytes inmutables de la metadata original. Triggers SQLite prohíben su borrado o una actualización no monotónica.
- `LoadActiveGeneration` lee desde una instantánea única y comprueba los hashes de los blobs, el manifiesto, root actual, high-water de roles activos y bytes fuente de high-water históricos incluso cuando el rol no aparece en la generación activa.
- Tests cubren rollback total ante fallo de escritura, migración con root-chain existente, re-apertura, delegaciones, rollback/equivocation, root drift, CAS, borrado/decremento SQL y corrupción de la evidencia de un rol omitido.

La matriz pre-merge del commit `c5fe257c22d19a6183b7d6b28031325a5539f01e` pasó 5/5 en [run 46095086164](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46095086164): Windows, macOS, Ubuntu, Race Detector y fuzzing. PR #25 se integró en `main` como `e4341ab`.

### Límites que permanecen tras PR #25

- `CommitGeneration` solo guarda metadata que el caller ya verificó; el paquete SQLite no verifica firmas TUF, threshold, hashes declarados por snapshot ni expiración.
- `TUFClient` todavía no crea estas generaciones automáticamente. Falta materializar staging a partir del cache exacto del updater, comprobar hashes/longitudes de snapshot y recolectar cada delegación requerida mediante `GetTargetInfo`.
- No se afirma que se haya resuelto el grafo completo de delegaciones del repositorio: la generación contiene los roles suministrados y verificados por el caller.
- Los tests actuales prueban re-apertura y rollback transaccional inyectando errores; falta terminación abrupta real entre las etapas, disco lleno y escritores en procesos separados.
- No se habilita `UnsafeLocalMode`, consulta offline ni anti-rollback fuerte frente a restauración integral del mismo disco.
## Contrato de generaciones SQLite v2 (PR #25, integrado)

PR #25 integra la primera implementación ejecutable del modelo de generaciones, todavía como capa de almacenamiento independiente del updater:

- La migración v1→v2 crea `generations`, `generation_metadata`, `generation_role_parents`, `role_high_water` y `active_generations` sin modificar ni eliminar la root-chain existente. La prueba de migración incluye una rotación root v2 preexistente y verifica que sus bytes, hash y enlace con el ancla se conservan.
- `CommitGeneration` requiere `timestamp`, `snapshot` y `targets`; acepta además roles delegados ya resueltos y verificados por `catalog`, junto con sus relaciones padre/hijo. Rechaza roles duplicados, relaciones ausentes, ciclos, tamaños excesivos y nombres no normalizados.
- La operación exige que la root propuesta coincida con latest-root y aplica CAS sobre la generación activa. En una sola transacción inserta la generación, los blobs y relaciones, actualiza high-water por rol y cambia `active_generations`.
- El high-water conserva versión, hash, expiración y `source_generation_id`; sus claves foráneas lo vinculan a los bytes inmutables de la metadata original. Triggers SQLite prohíben su borrado o una actualización no monotónica.
- `LoadActiveGeneration` lee desde una instantánea única y comprueba hashes de los blobs, manifiesto, root actual, high-water de roles activos y bytes fuente de high-water históricos incluso cuando un rol no aparece en la generación activa.
- Tests cubren rollback total ante fallo de escritura, migración con root-chain existente, re-apertura, delegaciones, rollback/equivocation, root drift, CAS, borrado/decremento SQL y corrupción de la evidencia de un rol omitido.

La matriz pre-merge del commit `c5fe257c22d19a6183b7d6b28031325a5539f01e` pasó 5/5 en [run 46095086164](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46095086164): Windows, macOS, Ubuntu, Race Detector y fuzzing. PR #25 se integró en `main` como `e4341ab`.

### Límites que permanecen tras PR #25

- `CommitGeneration` solo guarda metadata que el caller ya verificó; el paquete SQLite no valida firmas TUF, threshold, hashes declarados por snapshot ni expiración.
- `TUFClient` todavía no crea estas generaciones automáticamente. Falta materializar staging a partir de la caché exacta del updater, comprobar hashes/longitudes de snapshot y recolectar cada delegación requerida mediante `GetTargetInfo`.
- No se afirma que se haya resuelto el grafo completo de delegaciones del repositorio: la generación contiene los roles suministrados y verificados por el caller.
- Los tests actuales prueban re-apertura y rollback transaccional inyectando errores; falta terminación abrupta real entre etapas, disco lleno y escritores en procesos separados.
- No se habilita `UnsafeLocalMode`, consulta offline ni anti-rollback fuerte frente a restauración integral del mismo disco.