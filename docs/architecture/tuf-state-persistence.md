# Modelo de persistencia, anti-rollback y recuperación del catálogo TUF

- Estado: root-chain SQLite, replay persistente, store de generaciones, captura/revalidación online, staging privado, lock interproceso y recuperación de stages huérfanos implementados en la rama de trabajo (PR #22, #23, #25, #27, #29 y #31). Siguen pendientes las pruebas de fallos físicos y el modo offline.
- Fecha: 2026-11-28
- Relacionado: ADR-0015, ADR-0017, Issue #5 (M1.4), Issue #4 (M1.5).
- Alcance: metadata de confianza y caché del catálogo; no targets ejecutables.

## 1. Resumen

SOPTRAZ ya cuenta con una primera capa online de TUF basada en el `updater.Updater` oficial de go-tuf v2.5.0. El cliente recibe un `TrustAnchor` previamente fijado, resuelve roles delegados, verifica el descriptor artifact v1 y no descarga ni ejecuta el binario.

El contrato de generaciones SQLite está conectado al `TUFClient`: después de resolver un descriptor, el cliente captura bytes originales aceptados por go-tuf, los compara con su estado confiable, reproduce la cadena con `trustedmetadata` y activa el conjunto en una transacción. PR #29 agrega un workspace privado por refresh; al iniciar un updater persistente, copia allí los roles de la generación activa solo si su root coincide con la raíz durable actual. PR #31 serializa clientes entre procesos mediante un lock OS estable por store e ID de repositorio y limpia los stages huérfanos únicamente después de adquirir ese lock. Los nombres de stage incluyen el hash del repositorio, por lo que no se eliminan directorios de otra identidad que comparta la caché. El updater oficial sigue el flujo online y vuelve a validar la caché; ante un error se elimina el stage candidato y permanece la generación SQLite anterior. `Close()` elimina el workspace en cierres ordenados. Esto no constituye un modo offline; siguen pendientes fallos físicos y durabilidad ante problemas de almacenamiento.

El diseño separa cuatro propiedades que no son equivalentes:

1. **Autenticidad:** firmas, thresholds, hashes y delegaciones TUF prueban quién autorizó metadata/targets.
2. **Consistencia:** timestamp, snapshot y targets enlazan la vista del repositorio, evitando mezclar metadata de generaciones diferentes.
3. **Persistencia local:** la última generación completamente verificada se conserva de manera transaccional y se recupera tras cierre/reinicio.
4. **Anti-rollback fuerte:** recordar versiones superiores evita retroceder mientras ese recuerdo permanece intacto. Si un atacante restaura una imagen completa del disco que contiene tanto la caché como ese recuerdo, el estado local por sí solo no puede demostrar que fue retrocedido.

No se habilita el modo offline de producción ni se declara cerrado M1.4 con este documento.

## 2. Hechos actuales del código y de TUF

### 2.1 SOPTRAZ hoy

- `NewTUFClient` recibe un `TrustAnchor` ya fijado; no hace bootstrap de una raíz desconocida.
- `NewPersistentTUFClient` (PR #23, integrado) recibe un almacén SQLite abierto, registra el origen/pin y reproduce la cadena histórica desde el root bootstrap inmutable con `trustedmetadata.UpdateRoot` antes de crear el updater online.
- En modo persistente, un transporte captura únicamente respuestas `N.root.json` del mismo origen y reenvía exactamente esos bytes al fetcher de go-tuf. La capa catalog verifica de nuevo el prefijo y exige que su versión final coincida con la versión aceptada por el updater oficial antes del commit SQLite.
- Los roots validados se comprometen aun cuando una etapa posterior del refresh falle; el updater queda invalidado y una nueva instancia puede continuar desde el root durable. El constructor legacy `NewTUFClient` conserva el comportamiento de persistencia solo en memoria.
- El cliente crea el `Updater` oficial, ejecuta un refresh online, usa HTTPS excepto loopback explícito de tests y desactiva redirects.
- Las llamadas a `Updater` se serializan; la librería no es segura para concurrencia y permite un único `Refresh()` por instancia.
- Un refresh fallido invalida la instancia del updater en memoria para que la aplicación no continúe consultando metadata vieja de esa instancia.
- PR #20 conserva también el último root que go-tuf dejó verificado como base de los refreshes siguientes dentro del mismo proceso. Si el refresh falla después de una rotación, el updater queda invalidado para consultas, pero la raíz verificada se conserva en memoria. No sobrevive todavía a reinicios ni a la recreación de la aplicación.
- El updater puede persistir archivos de metadata en el directorio de caché. La implementación oficial escribe cada metadata mediante archivo temporal, cierra, renombra y relee para comparar bytes. Esto protege la escritura individual frente a truncamiento lógico, pero no equivale a una transacción única de todo el conjunto root/timestamp/snapshot/targets/delegaciones ni documenta por sí solo una garantía de durabilidad frente a pérdida súbita de energía.
- El esquema SQLite v2 almacena generaciones inmutables, metadata por rol/delegación, high-water con procedencia y `active_generation` (PR #25). PR #27 conecta `TUFClient` con ese store: tras `ResolveDescriptor` exitoso, lee los bytes originales de la caché de go-tuf, los compara con los objetos confiables en memoria, revalida con `trustedmetadata` y compromete la generación. PR #29 mueve el directorio de trabajo de go-tuf a un stage privado por intento y restaura desde SQLite la última generación compatible con la root actual; la validación sigue siendo online y usa el updater oficial. No existe todavía recuperación offline ni limpieza segura de stages huérfanos tras crash.

### 2.2 Reglas del protocolo

El flujo TUF requiere metadata firmada por los roles correctos, versiones monotónicas, expiración, hashes y tamaños enlazados entre timestamp, snapshot y targets. El cliente debe persistir metadata aceptada. La expiración ayuda a detectar metadata congelada, pero solo limita la ventana de frescura: no equivale a conocer el estado remoto actual cuando el equipo está desconectado.

La rotación de root debe ser consecutiva: cada root siguiente es verificado contra el root confiable anterior y contra el threshold del nuevo root. SOPTRAZ ya tiene un test de integración del updater oficial que cubre el salto v1→v2 con ambos thresholds y rechaza una rotación sin firma de la raíz anterior.

## 3. Modelo de amenazas

### Protecciones que TUF más el store local pueden ofrecer

- Alteración de metadata sin claves autorizadas: rechazada por firmas, hashes y schemas TUF.
- Mezcla de snapshot/targets de repositorios o generaciones distintas: rechazada por hashes/versiones enlazadas.
- Reproducción de una versión anterior mientras el high-water confiable permanece intacto: rechazada por versiones monotónicas.
- Metadata vencida o congelada más allá de su expiración: rechazada.
- Interrupción durante la preparación de una actualización: la generación anterior confirmada permanece disponible si la nueva todavía no fue comprometida.
- Corrupción parcial de un archivo/registro: checksum, verificación TUF y validación del manifiesto deben fallar de manera cerrada.

### Riesgos que no se resuelven únicamente en el mismo disco

- Restaurar un backup antiguo que incluya a la vez metadata, high-water marks, base local y claves/software de la aplicación.
- Retroceder el reloj del sistema para hacer parecer vigente metadata expirada, cuando no hay una fuente temporal confiable.
- Comprometer el sistema operativo con privilegios suficientes para modificar el almacén y su proceso de validación.
- Comprometer las claves de firma del repositorio por encima de sus thresholds.

SOPTRAZ no debe afirmar que un registro SQLite, un HMAC almacenado en el mismo equipo o un hash chain por sí solos impiden la restauración integral de una imagen de disco antigua. Un secreto local también puede restaurarse junto con el backup que lo contiene.

### Niveles de assurance a informar

| Nivel | Ancla anti-rollback | Garantía declarable |
|---|---|---|
| Local | High-water state en el almacén protegido de SOPTRAZ | Detecta retroceso si el almacén vigente se conserva; no detecta restauración integral del mismo disco |
| Hardware | Contador/estado monotónico protegido por plataforma cuando exista soporte comprobable | Puede detectar restauración del disco por debajo del valor anclado; depende de soporte, provisionamiento, permisos y pruebas del dispositivo |
| Externo | Witness remoto/HUB o servicio de transparencia con último estado visto | Puede detectar retroceso respecto del último checkpoint externo conocido; requiere conectividad en el momento de consultar/confirmar |

La falta de un ancla de hardware o externa no debe disfrazarse como anti-rollback fuerte. El reporte debe nombrar el nivel real y la limitación.

## 4. Persistencia por generaciones

### 4.1 Staging aislado implementado para el flujo online (PR #29)

Cada refresh persistente trabaja sobre un directorio temporal privado creado bajo la raíz local de caché. Se inicializa con los roles de la generación activa de SQLite solo cuando el root version/hash coincide con la root durable actual; si no hay generación compatible, comienza vacío. `UpdaterConfig.LocalMetadataDir` apunta al stage, por lo que el updater oficial de go-tuf escribe allí y no altera la carpeta base ni el snapshot comprometido. El flujo sigue siendo online: go-tuf valida la metadata local y consulta el repositorio remoto según el protocolo normal.

Si la inicialización o el refresh falla, el stage de ese intento se descarta y la generación activa anterior no cambia. `TUFClient.Close()` elimina el workspace en un cierre ordenado. PR #31 incorpora un lock OS estable por ruta de trust store e ID de repositorio, mantenido durante la vida del cliente. Si un proceso termina abruptamente, el OS libera el lock; el siguiente constructor lo adquiere antes de inspeccionar y eliminar stages huérfanos. El prefijo de cada stage incluye el hash del ID de repositorio, por lo que una caché compartida no permite que la limpieza de un repositorio borre stages de otro. Los archivos de lock permanecen en disco intencionalmente: borrar un lockfile mientras otro proceso puede tenerlo abierto crearía una carrera de inode. La materialización desde SQLite no habilita `UnsafeLocalMode` ni consultas offline.

- Un solo escritor por repositorio, protegido por lock OS entre procesos; el test de integración mantiene un proceso hijo bloqueando el repositorio, comprueba que un segundo proceso es rechazado, lo termina por fuerza y valida que el lock se libera.
- Staging en el mismo volumen del store para que los movimientos de archivos no crucen volúmenes.
- Límite de bytes y cantidad de metadata antes de materializar.
- Nombres de roles normalizados, sin traversal ni nombres de archivo arbitrarios.
- No permitir symlinks/reparse points, archivos especiales ni directorios controlados por usuarios no confiables.
- Si falla refresh, verificación, delegación solicitada, escritura o lectura posterior, se descarta staging y se conserva la generación anterior.
- La generación nueva no se considera aceptada solo porque `Refresh()` retorne éxito: para los targets delegados que la aplicación va a usar, debe haberse ejecutado `GetTargetInfo()` y completado la cadena de delegación correspondiente.

### 4.2 Registro de generación

Cada generación comprometida por `CommitGeneration` tiene un manifiesto SHA-256 determinista sobre repositorio, IDs, root, roles, versiones, expiración, bytes y relaciones padre/hijo. PR #27 suministra esos roles desde la caché del updater oficial únicamente después de compararlos con el estado confiable y reproducir la cadena con `trustedmetadata`. El manifiesto cubre:

- `repository_id` y origen configurado del repositorio.
- `generation_id` único y número monotónico local.
- Versión y SHA-256 exacto de root, timestamp, snapshot, targets y cada metadata delegada cargada.
- `expires` y `version` validados por cada rol.
- Roles padre/delegados y cadena de delegación para los targets consultados.
- Fecha de refresh y hora de referencia usada en la validación.
- Versión de SOPTRAZ, versión de go-tuf y versión del formato de persistencia.
- High-water marks previos y nuevos que se comprometen con esa misma generación.
- Resultado de validación y número de bytes/archivos.

No confiar en fechas locales como prueba de orden; usar versiones TUF firmadas. El identificador local de generación es para recuperación y concurrencia, no sustituye los contadores TUF.

### 4.3 Commit atómico (decisión tomada por ADR-0017)

Se selecciona un SQLite separado (`catalog-trust.sqlite`), no `audit.sqlite`. PR #25 integra el esquema v2 y la API transaccional: BLOBs para metadata original, manifiesto por generación, relaciones de delegación, high-water por rol con procedencia inmutable y puntero `active_generation`. PR #27 conecta `TUFClient` a esta API: captura metadata solo tras una resolución exitosa, compara los bytes de caché con los objetos confiables de go-tuf, reproduce roles superiores y delegaciones recorridas mediante `trustedmetadata` y después llama a `CommitGeneration`. La transacción valida root actual, monotonicidad y CAS, actualiza high-water y cambia el puntero; si cualquier operación falla, todo revierte.

La cadena de root tiene un commit separado e independiente:
- Después de un intento de refresh, la capa `catalog` recupera los roots que go-tuf efectivamente verificó y los reproduce desde el trust anchor independiente con `trustedmetadata.UpdateRoot`.
- Una transacción agrega los roots consecutivos validados y actualiza latest-root version/hash mediante compare-and-swap.
- Este commit se realiza aun cuando timestamp/snapshot/targets posteriores hayan fallado: una raíz ya verificada debe sobrevivir ese fallo.
- La generación activa anterior permanece almacenada, pero no es elegible offline si su root version/hash ya no coincide con latest-root. El catálogo queda en estado `refresh_required` hasta comprometer una generación completa bajo la raíz actual.

El proceso online restaura la generación activa compatible en un staging privado, ejecuta el updater oficial y resuelve los roles delegados requeridos. Solo después de validar tamaños, nombres de rol, versiones, hashes, expiraciones y manifiesto, `CommitGeneration` actualiza SQLite. El filesystem temporal es solo espacio de trabajo; SQLite sigue siendo el registro autoritativo. El stage puede descartarse y reconstruirse desde la última generación confirmada.

La base usa `modernc.org/sqlite` sin CGO, una conexión de escritura, transacciones `BEGIN IMMEDIATE`, foreign keys, busy timeout acotado, `synchronous=FULL`, `trusted_schema=OFF` y journaling rollback `DELETE` explícito inicialmente. El esquema v2 conserva la root-chain v1 e incorpora tablas append-only para generaciones, metadata y delegaciones, además de high-water con referencia a los bytes fuente y triggers monotónicos. El archivo queda separado de auditoría; la carpeta de datos y ACL se configuran con mínimo privilegio. No se habilita WAL hasta contar con pruebas de recovery/sidecar en Windows, macOS y Linux.

No combinar una transacción SQLite y un rename de filesystem suponiendo que ambos forman una transacción distribuida. La actualización del root chain puede confirmarse sin avanzar la generación; la generación activa solo avanza cuando metadata completa y high-water están en la misma transacción.

La base conserva todos los roots intermedios requeridos para reproducir la cadena desde el pin inicial. La integridad criptográfica se revalida al cargar; no se confía en el root final aislado. Un cambio de pin de bootstrap requiere migración explícita. El store no debe importarse ni sincronizarse como datos ordinarios de Field → Hub.

## 5. High-water marks y protección anti-rollback

La base persistente deberá guardar, por rol y repositorio:

- último root version confiable y hash exacto;
- último timestamp version/hash/expiry;
- último snapshot version/hash/expiry;
- último targets version/hash/expiry;
- última versión/hash/expiry de cada rol delegado que se haya autenticado;
- relación firmada snapshot → targets delegados;
- generación local que comprometió el estado.

Reglas:

- Nunca aceptar una versión menor que el high-water confiable.
- Si una versión es igual, el contenido debe ser exactamente el mismo que el previamente aceptado; una misma versión con bytes diferentes es corrupción/conflicto.
- Root solo avanza mediante rotaciones consecutivas y thresholds TUF.
- No bajar high-water al recuperar datos de un backup.
- Una generación vieja se puede conservar para análisis, pero no restaurar como activa.
- Si el almacén no puede demostrar que el estado actual cumple monotonicidad, bloquear la confianza del catálogo hasta un refresh online verificado o enrolamiento/recuperación de confianza.
- Si la política usa un ancla hardware/externa, cualquier diferencia entre el high-water del store y el ancla debe disparar bloqueo y procedimiento de recuperación; no auto-reparar reduciendo el contador.

Un SHA-256 local detecta cambios respecto de un hash esperado, pero no impide que un atacante reemplace a la vez el archivo y el hash persistido. La autenticidad proviene de TUF; el high-water y el ancla anti-rollback cumplen otra función.

## 6. Política offline: separada del refresh online

No configurar `UpdaterConfig.UnsafeLocalMode` como solución productiva. La documentación de go-tuf dice expresamente que ese flujo se aparta del workflow TUF. No debe confundirse con modo offline seguro.

SOPTRAZ mantendrá dos operaciones diferentes:

- **Refresh online:** busca metadata nueva y ejecuta el workflow completo root → timestamp → snapshot → targets; carga delegaciones bajo demanda. Solo una generación totalmente aceptada se compromete.
- **Consulta offline:** lee exclusivamente una generación previamente comprometida y ejecuta un validador local explícito que vuelve a comprobar root chain, thresholds, expiración, versiones monotónicas, hashes, delegaciones y high-water marks. No descarga ni modifica el estado confiable.

El verificador offline se implementará con las primitivas oficiales de go-tuf (`trustedmetadata`), no con un parser criptográfico propio. Tendrá que validar root rotations consecutivas almacenadas, timestamp, snapshot, targets y delegaciones solicitadas. La sesión actual `MetadataSession` solo cubre metadata top-level y no basta todavía para una consulta offline con delegaciones.

### Condiciones para ofrecer resultados offline

- La generación fue comprometida previamente en una sesión online exitosa.
- La cadena de root llega a un trust anchor fijado/enrolado.
- Cada metadata requerida está presente, íntegra, enlazada por hash/length y no expirada.
- Los high-water marks coinciden o avanzan; no hay regresión ni versión repetida con bytes diferentes.
- La hora de referencia es razonable y no hay indicios de retroceso del reloj no resueltos.
- Se puede validar el target mediante su delegación firmada; metadata ausente o delegación no resoluble bloquea ese target.
- El informe muestra la hora de última actualización, expiración, generación y nivel de assurance anti-rollback.

Un resultado offline significa “metadata previamente autenticada y todavía dentro de su período de validez”, no “el servidor no publicó algo nuevo” ni “se sabe que esta es la versión más reciente”. Al terminar la expiración, no se acepta por conveniencia: el catálogo queda no disponible hasta refresh online.

## 7. Root bootstrap, custodia y rotación

El root inicial se distribuye en un canal separado del catálogo: embebido en una release oficial firmada de SOPTRAZ o enrolado mediante un flujo de instalación verificado fuera de banda. Un root descargado desde el mismo origen que todavía no se confía no puede bootstrapearse a sí mismo.

La política de producción deberá definir antes de generar claves:

- cuántos custodios controlan el root y cuántas firmas forman el threshold;
- cómo se mantienen claves privadas offline/HSM y cómo se documenta su recuperación;
- quién administra timestamp online y cómo se restringe su privilegio;
- separación de claves root/snapshot/targets/timestamp;
- procedimiento de rotación planificada y de compromiso/revocación;
- canal para distribuir una nueva raíz cuando la app instalada está demasiado antigua;
- auditoría de operaciones de firma y prueba de restauración de custodios.

No se generan claves privadas de producción en tests, issues, conversaciones, repositorio o CI. Los fixtures de CI usan claves aleatorias efímeras sin relación con la custodia real. No se fija todavía un threshold numérico sin definir custodios, operación y capacidad de recuperación.

## 8. Recuperación y fallos

| Escenario | Resultado esperado |
|---|---|
| Crash durante refresh/staging | Descartar staging; continuar con generación anterior si sigue vigente |
| Crash durante commit SQLite | Recuperación transaccional: queda activa la generación anterior o la nueva, nunca mezcla |
| Metadata faltante/corrupta | No reparar inventando metadata; refresh online o bloquear |
| Timestamp/snapshot/targets expirados | No habilitar catálogo offline; solicitar refresh online |
| Versión menor al high-water | Rechazar rollback |
| Igual versión, diferente hash | Rechazar conflicto/corrupción |
| Backup antiguo del store local | Comparar con ancla externa/hardware si existe; de lo contrario reportar que el rollback integral no se detecta con garantía |
| Root con firma/threshold inválido | Rechazar; preservar trust state previo |
| Falta de red | Consultar solo generación offline validable y no expirada; no ejecutar UnsafeLocalMode |
| Fallo del refresh más nuevo | No hacer downgrade de la confianza ni servir metadata expirada; mantener el estado anterior solo si la política vigente aún lo permite |
| Corrupción del reloj | Bloquear catálogo offline cuando no puede establecerse vigencia |

No se restaurará automáticamente una generación vieja para “hacer que el programa arranque”. La recuperación debe preservar integridad antes que disponibilidad.

## 9. Pruebas requeridas antes de producción

### Pruebas deterministas

- Reinicio del proceso después de cada etapa de staging/commit.
- Terminación abrupta simulada entre escritura, readback y commit.
- Corromper/truncar individualmente root, timestamp, snapshot, targets y delegaciones.
- Cambiar metadata manteniendo mismo número de versión.
- Restaurar una generación anterior mientras el high-water actual permanece.
- Restaurar conjuntamente generación + high-water (demostrar el límite sin ancla hardware/externa).
- Alterar hora hacia delante/atrás y casos de expiración en el límite.
- Rotación root N→N+1, múltiple rotación, threshold y firma duplicada.
- Delegación ausente, hash/length incorrectos, delegación no autorizada o no terminating.
- Transacciones SQLite interrumpidas, WAL recovery, disco lleno, permisos insuficientes y dos procesos concurrentes.
- Reinicio con caché incompleta o versiones metadata inconsistentes.
- Pruebas en Windows, macOS y Ubuntu; race detector, fuzzing de metadata y pruebas específicas de persistencia.

### Pruebas de aceptación

- La generación activa nunca contiene roles de distintas actualizaciones confirmadas.
- El commit de high-water y generación activa es atómico.
- No hay downgrade por recuperación de backups.
- Offline no usa UnsafeLocalMode y solo acepta metadata previamente autenticada, íntegra, vigente y monotónica.
- El sistema comunica explícitamente el límite de rollback de disco completo cuando no hay ancla hardware/externa.
- No se descarga ni ejecuta ningún target desde el catálogo.
- El supervisor M1.5 sigue siendo requisito para cualquier ejecución.

## 10. Plan de implementación propuesto

1. Mantener la implementación online como prototipo reforzado; no marcarla como production-ready/offline.
2. [x] Caracterizar el comportamiento de go-tuf v2.5.0 para root, roles superiores y delegaciones bajo demanda; continuar ampliando pruebas de recuperación y fallos.
3. [x] Seleccionar almacenamiento SQLite separado y contrato de root chain/generaciones/high-water mediante ADR-0017.
4. [x] Implementar store de root-chain (PR #22), replay/commit persistente de `TUFClient` (PR #23), store de generaciones/high-water (PR #25) y captura/revalidación online desde `TUFClient` (PR #27).
5. [ ] Implementar staging aislado por generación antes del refresh y restauración validada desde SQLite; la caché oficial deja de ser el único espacio de trabajo pero nunca se promueve por presencia solamente.
6. [ ] Añadir tests de terminación abrupta real, disco lleno y dos procesos concurrentes; la reapertura ordenada, replay TUF y rollback transaccional inyectado ya están cubiertos.
7. [ ] Implementar el verificador offline local usando `trustedmetadata` y validación de delegaciones; no usar `UnsafeLocalMode`.
8. [ ] Añadir ancla monotónica por plataforma o witness externo cuando esté disponible, manteniendo un nivel local explícito cuando no lo esté.
9. [ ] Completar SBOM, avisos/licencias y política de actualización de go-tuf/Sigstore.
10. [ ] Recién entonces revisar M1.4 como candidato a aceptación.

## 11. Referencias oficiales

- [TUF Specification — update timestamp role](https://theupdateframework.github.io/specification/latest/#update-the-timestamp-role) — threshold, rollback, freeze, persistencia de timestamp.
- [TUF Specification — update snapshot role](https://theupdateframework.github.io/specification/latest/#update-the-snapshot-role) — integridad y versiones monotónicas de metadata enlazada.
- [TUF Specification — update targets role](https://theupdateframework.github.io/specification/latest/#update-the-targets-role) — hashes, versión, expiración y delegaciones.
- [TUF roles and metadata](https://theupdateframework.io/docs/metadata/) — responsabilidades de root, timestamp, snapshot, targets y delegaciones.
- [go-tuf v2.5.0 updater.go](https://github.com/theupdateframework/go-tuf/blob/v2.5.0/metadata/updater/updater.go) — flujo del updater, refresh, UnsafeLocalMode y persistencia individual de metadata.
- [TUF security scope](https://github.com/theupdateframework/specification/blob/master/governance/02-scope.md) — amenazas cubiertas como rollback, freeze y mix-and-match.

Este documento registra el diseño de seguridad y las decisiones que faltan. La persistencia durable de root-chain ya está implementada; este documento no habilita todavía el modo offline ni una garantía hardware anti-rollback.


## 12. Estado de implementación tras PR #20, PR #22, PR #23, PR #25, PR #27 y PR #29

- PR #20 integrado como `1bf7e13`: conserva el último root verificado entre refreshes dentro del mismo proceso.
- PR #22 integra root-chain append-only SQLite con pin/origen, hashes exactos, enlaces consecutivos, CAS y lectura consistente.
- PR #23 integrado como `1988db9` añade `NewPersistentTUFClient`: reproduce roots desde el pin bootstrap y persiste rotaciones aceptadas por go-tuf incluso si falla metadata posterior.
- PR #25 integrado como `e4341ab` añade esquema SQLite v2, generaciones inmutables, roles/delegaciones, manifiesto SHA-256, high-water con procedencia inmutable y activación CAS transaccional.
- PR #27 integrado como `4983f7d` conecta el capturador existente con `TUFClient`: después de una resolución exitosa, captura bytes originales, compara con el estado confiable, reproduce roles superiores y delegaciones recorridas con `trustedmetadata` y compromete de manera idempotente. Si falla el commit, la resolución persistente falla cerrada.
- PR #29 agrega un workspace privado por refresh persistente, rehidratado desde SQLite solo cuando la generación activa coincide con la root durable. El updater oficial revalida online y no escribe en la carpeta base; un refresh fallido descarta el stage. `TUFClient.Close()` limpia el workspace. La caída abrupta, stages huérfanos, procesos concurrentes y modo offline siguen fuera del alcance.
- La matriz pre-merge de PR #27 pasó 5/5 en [run 46097598736](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46097598736); el CI post-merge de `main` también pasó 5/5 en [run 46097718344](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46097718344): Windows, macOS, Ubuntu, Race Detector y fuzzing.
- La generación contiene metadata top-level y los roles delegados recorridos por los descriptores resueltos; no afirma que todo el grafo del repositorio esté cargado. No se crea una generación en el constructor ni por `Refresh()` sin descriptores resueltos.
- Falta limpiar stages huérfanos tras una terminación abrupta, probar fallos de disco/espacio, proteger escritores multi-proceso y definir un validador offline explícito. `UnsafeLocalMode` sigue deshabilitado.
- No existe protección fuerte frente a restauración de una imagen antigua completa del mismo disco sin TPM/contador monotónico o witness externo. Bootstrap y custodia de claves de producción siguen pendientes.
