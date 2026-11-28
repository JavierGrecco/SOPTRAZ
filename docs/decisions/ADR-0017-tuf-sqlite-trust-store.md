# ADR-0017: Almacén durable de confianza TUF en SQLite

- Estado: Aceptada. Root-chain persistente (PR #22/#23), store de generaciones SQLite v2 (PR #25) e integración online de captura/revalidación desde `TUFClient` (PR #27) están integrados en `main`. La generación se compromete después de resolver descriptores y solo incluye roles delegados realmente recorridos. Siguen pendientes la restauración offline desde generaciones, crash/restart abrupto, resiliencia ante fallos físicos, bootstrap de producción y ancla anti-rollback externa.
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
6. [ ] Integrar el updater online con staging: obtener bytes exactos autenticados por go-tuf, resolver los roles delegados que la aplicación necesita y solo entonces llamar a `CommitGeneration`; no declarar completo un grafo que no se haya recorrido.
7. [ ] Añadir tests de terminación abrupta real, crash/restart, corrupción, rollback, disco lleno y concurrencia entre procesos en Windows/macOS/Linux.
8. [ ] Implementar el validador offline explícito y su reporte de assurance.
9. [ ] Completar SBOM, avisos/licencias y política de actualización de go-tuf/Sigstore.
La implementación no se considera production-ready hasta que todas las pruebas de recuperación y anti-rollback estén verdes y el procedimiento de bootstrap/custodia de root esté aprobado.

## Implementación integrada y evidencia

### PR #22 — Root-chain SQLite

- `internal/storage/sqlitetuf` guarda raíces históricas vinculadas al origen y al pin bootstrap, con hashes exactos, secuencia consecutiva, append-only, CAS y lectura consistente.
- El store no verifica firmas TUF: solo recibe raíces autenticadas por `catalog`.

### PR #23 — `NewPersistentTUFClient`

- Registra origen y pin bootstrap; carga la cadena SQLite y reproduce cada root consecutiva con `trustedmetadata.UpdateRoot` antes de crear el updater oficial.
- Captura respuestas versionadas `N.root.json` del origen configurado y persiste únicamente cuando la versión alcanzada por go-tuf coincide con la cadena candidata.
- Conserva una rotación ya aceptada aunque falle timestamp/snapshot/targets más adelante; la instancia del updater queda invalidada y un proceso posterior vuelve a autenticar la cadena desde el pin original.
- Integrado en `main` como `1988db9`.

### PR #25 — Generaciones SQLite v2

- Migra v1→v2 sin perder una root-chain preexistente e incorpora `generations`, `generation_metadata`, `generation_role_parents`, `role_high_water` y `active_generations`.
- `CommitGeneration` valida root actual, CAS, roles superiores, delegaciones declaradas, monotonicidad por rol y límites; escribe blobs, relaciones, high-water y puntero activo en una transacción.
- El high-water conserva `source_generation_id` vinculado a bytes inmutables; triggers SQLite impiden borrado y downgrade directo.
- `LoadActiveGeneration` lee una instantánea coherente y comprueba hash de blobs, manifiesto, root actual y evidencia histórica incluso para roles omitidos.
- Integrado en `main` como `e4341ab`.

### PR #27 — Captura de generaciones desde `TUFClient`

- Tras un `ResolveDescriptor` persistente exitoso, `TUFClient` reúne los paths resueltos y llama al capturador existente `CommitVerifiedGeneration` bajo el mismo mutex que serializa al updater.
- El capturador lee los bytes originales almacenados por go-tuf, rechaza archivos no regulares, symlinks y tamaños fuera de límite, y exige que los documentos parseados coincidan con el estado confiable en memoria.
- Reproduce timestamp → snapshot → targets y las delegaciones recorridas mediante `trustedmetadata`, incluyendo hashes/longitudes del snapshot, threshold, versiones y expiración.
- Solo conserva roles delegados recorridos por descriptores exitosamente resueltos; no afirma haber descubierto todo el grafo de delegaciones del repositorio.
- Compara contra la generación activa y evita crear una nueva cuando root, bytes, versiones, expiraciones y relaciones son idénticos. Si la persistencia falla, la resolución falla cerrada y el updater se invalida.
- La generación no se crea en el constructor ni por un `Refresh()` aislado: se crea cuando existe al menos un descriptor autenticado. Esto evita presentar una generación parcial como catálogo completo.
- Integrado en `main` como `4983f7d`.

### Evidencia de CI

- PR #25: matriz pre-merge 5/5 en [run 46095086164](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46095086164); CI post-merge 5/5 en [run 46095255858](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46095255858).
- PR #27: matriz pre-merge 5/5 en [run 46097598736](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46097598736); CI post-merge 5/5 en [run 46097718344](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46097718344). Ambos cubren Windows, macOS, Ubuntu, Race Detector y fuzzing.

## Límites y siguientes hitos

- El updater online todavía usa su caché de archivos como fuente de trabajo; PR #27 persiste los bytes aceptados en SQLite después de resolver descriptores, pero aún no construye un staging aislado e inmutable antes del refresh.
- No existe todavía un flujo que materialice una generación desde SQLite en un reinicio y ejecute un verificador offline explícito. `UnsafeLocalMode` permanece deshabilitado.
- La generación contiene metadata top-level y roles delegados recorridos por los descriptores resueltos; los roles nunca consultados no se presentan como verificados ni como parte de un grafo completo.
- La re-apertura ordenada, la integridad de blobs y el rollback transaccional inyectado están cubiertos. Faltan pruebas de terminación abrupta entre etapas, disco lleno, errores de fsync/sidecar y escritores concurrentes en procesos distintos.
- El estado SQLite local no protege frente a restaurar una imagen completa antigua del mismo disco. Sigue pendiente un TPM/contador monotónico o witness externo para elevar el assurance anti-rollback.
- El bootstrap de confianza y la custodia/rotación de claves de producción siguen abiertos. No se declara M1.4 cerrado hasta completar recuperación, bootstrap y verificación offline.
