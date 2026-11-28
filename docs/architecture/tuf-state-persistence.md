# Modelo de persistencia, anti-rollback y recuperación del catálogo TUF

- Estado: modelo de amenazas vigente; root-chain SQLite e integración persistente de `TUFClient` integradas en `main` (PR #22 y #23); generaciones completas y modo offline pendientes.
- Fecha: 2026-11-28
- Relacionado: ADR-0015, ADR-0017, Issue #5 (M1.4), Issue #4 (M1.5).
- Alcance: metadata de confianza y caché del catálogo; no targets ejecutables.

## 1. Resumen

SOPTRAZ ya cuenta con una primera capa online de TUF basada en el `updater.Updater` oficial de go-tuf v2.5.0. El cliente recibe un `TrustAnchor` previamente fijado, resuelve roles delegados, verifica el descriptor artifact v1 y no descarga ni ejecuta el binario.

La próxima etapa no consiste en guardar `root.json` en una carpeta. Debe preservar una **generación coherente de metadata autenticada**, su historial de versiones y un registro de confianza que sobreviva reinicios y fallos parciales.

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
- El repositorio de metadata actual es una carpeta configurada en `TUFClientConfig`; todavía no hay un almacén de generaciones y high-water marks separado.

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

## 4. Propuesta de persistencia por generaciones

### 4.1 Staging aislado

Cada intento de actualización trabaja sobre un directorio temporal exclusivo de generación. Se parte de una copia de la última generación aceptada y se construye allí el estado candidato. El updater oficial solo escribe dentro de ese staging; nunca modifica la generación actualmente confirmada.

- Un solo escritor por repositorio, protegido por lock de proceso y coordinación entre instancias.
- Staging en el mismo volumen del store para que los movimientos de archivos no crucen volúmenes.
- Límite de bytes y cantidad de metadata antes de materializar.
- Nombres de roles normalizados, sin traversal ni nombres de archivo arbitrarios.
- No permitir symlinks/reparse points, archivos especiales ni directorios controlados por usuarios no confiables.
- Si falla refresh, verificación, delegación solicitada, escritura o lectura posterior, se descarta staging y se conserva la generación anterior.
- La generación nueva no se considera aceptada solo porque `Refresh()` retorne éxito: para los targets delegados que la aplicación va a usar, debe haberse ejecutado `GetTargetInfo()` y completado la cadena de delegación correspondiente.

### 4.2 Registro de generación

Cada generación confirmada tendrá un manifiesto de estado (contrato aún no implementado) con:

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

Se selecciona un SQLite separado (`catalog-trust.sqlite`), no `audit.sqlite`. El almacén utiliza BLOBs para metadata original, un manifiesto por generación, high-water marks por rol y un puntero `active_generation`. El commit de una generación completa es una sola transacción: inserta generation + role metadata, valida monotonicidad, actualiza high-water y cambia active generation. Si cualquier operación falla, todo revierte.

La cadena de root tiene un commit separado e independiente:
- Después de un intento de refresh, la capa `catalog` recupera los roots que go-tuf efectivamente verificó y los reproduce desde el trust anchor independiente con `trustedmetadata.UpdateRoot`.
- Una transacción agrega los roots consecutivos validados y actualiza latest-root version/hash mediante compare-and-swap.
- Este commit se realiza aun cuando timestamp/snapshot/targets posteriores hayan fallado: una raíz ya verificada debe sobrevivir ese fallo.
- La generación activa anterior permanece almacenada, pero no es elegible offline si su root version/hash ya no coincide con latest-root. El catálogo queda en estado `refresh_required` hasta comprometer una generación completa bajo la raíz actual.

El proceso online materializa la generación activa en un staging temporal, ejecuta el updater oficial y resuelve los roles delegados requeridos. Solo después de validar tamaños, nombres de rol, versiones, hashes, expiraciones y manifiesto, se compromete la generación. El filesystem de staging no es el registro autoritativo.

La base usa `modernc.org/sqlite` sin CGO, una conexión de escritura, transacciones `BEGIN IMMEDIATE`, foreign keys, busy timeout acotado, `synchronous=FULL`, `trusted_schema=OFF` y journaling rollback `DELETE` explícito inicialmente. El archivo queda separado de auditoría; la carpeta de datos y ACL se configuran con mínimo privilegio. No se habilita WAL hasta contar con pruebas de recovery/sidecar en Windows, macOS y Linux.

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

1. Mantener la implementación online actual como prototipo; no marcarla como production-ready/offline.
2. Añadir tests que caractericen exactamente cómo go-tuf v2.5.0 lee/persiste root y roles, incluidas delegaciones y recuperación tras fallo.
3. [x] Seleccionar almacenamiento SQLite separado y contrato de root chain/generaciones/high-water mediante ADR-0017.
4. [x] Implementar el store aislado para root-chain (PR #22) y conectar el replay/commit persistente con `TUFClient` (PR #23, integrado). [ ] Completar el store de generaciones, high-water por rol, commits atómicos y tests de crash/restart.
5. Implementar loader online basado en staging temporal e importar al store solo después de validar completamente.
6. Implementar el verificador offline local usando trustedmetadata y validación de delegaciones; no usar UnsafeLocalMode.
7. Añadir ancla monotónica por plataforma o witness externo cuando esté disponible, manteniendo un nivel local explícito cuando no lo esté.
8. Completar SBOM, avisos/licencias y política de actualización de go-tuf/Sigstore.
9. Recién entonces revisar M1.4 como candidato a aceptación.

## 11. Referencias oficiales

- [TUF Specification — update timestamp role](https://theupdateframework.github.io/specification/latest/#update-the-timestamp-role) — threshold, rollback, freeze, persistencia de timestamp.
- [TUF Specification — update snapshot role](https://theupdateframework.github.io/specification/latest/#update-the-snapshot-role) — integridad y versiones monotónicas de metadata enlazada.
- [TUF Specification — update targets role](https://theupdateframework.github.io/specification/latest/#update-the-targets-role) — hashes, versión, expiración y delegaciones.
- [TUF roles and metadata](https://theupdateframework.io/docs/metadata/) — responsabilidades de root, timestamp, snapshot, targets y delegaciones.
- [go-tuf v2.5.0 updater.go](https://github.com/theupdateframework/go-tuf/blob/v2.5.0/metadata/updater/updater.go) — flujo del updater, refresh, UnsafeLocalMode y persistencia individual de metadata.
- [TUF security scope](https://github.com/theupdateframework/specification/blob/master/governance/02-scope.md) — amenazas cubiertas como rollback, freeze y mix-and-match.

Este documento registra el diseño de seguridad y las decisiones que faltan. La persistencia durable de root-chain ya está implementada; este documento no habilita todavía el modo offline ni una garantía hardware anti-rollback.


## 12. Estado de implementación tras PR #20, PR #22 y PR #23

- PR #20 integrado como `1bf7e13`: el último root TUF verificado se conserva entre refreshes de la misma instancia y sobrevive a un fallo posterior de timestamp/snapshot/targets.
- PR #22 integrado en `main`: el paquete `internal/storage/sqlitetuf` persiste la cadena histórica de root en un SQLite separado, con ancla/origen fijados, hashes exactos, enlaces consecutivos, append-only, CAS transaccional y lectura consistente.
- PR #23 integrado como `1988db9` añade `NewPersistentTUFClient`: replay de cada root desde el pin bootstrap inmutable, captura acotada de las respuestas versionadas y commit durable únicamente si go-tuf alcanzó la misma versión. Conserva raíces válidas aunque falle metadata posterior y las recupera con una nueva instancia/caché vacía.
- La matriz definitiva pre-merge pasó 5/5 en [run 46093570302](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46093570302) para el commit `c75ec36e8891bec608d276883c70b04b4c321229`: Windows, macOS, Ubuntu, Race Detector y fuzzing. El CI post-merge también pasó 5/5 en [run 46093718592](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46093718592) sobre `main` (`1988db9`).
- Persistencia de timestamp/snapshot/targets/delegaciones por generación, high-water por rol, staging, `active_generation`, recuperación tras terminación abrupta y validador offline siguen pendientes. No se habilita `UnsafeLocalMode`.
- El nivel de assurance local no protege frente a restauración integral de una imagen antigua del mismo disco sin TPM/contador monotónico o witness externo; el bootstrap/custodia de claves para producción sigue pendiente.
