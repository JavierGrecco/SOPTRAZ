# ADR-0019: Verificación offline explícita del catálogo TUF

- Estado: Aceptada como arquitectura; implementación pendiente.
- Fecha: 2026-11-28
- Relacionada: ADR-0015, ADR-0017, Issue #5 (M1.4), Issue #4 (M1.5).
- Alcance: validar metadata TUF previamente comprometida y descriptor de un artefacto. No descarga, instala ni ejecuta herramientas.

## Contexto

SOPTRAZ ya utiliza el `updater.Updater` oficial de go-tuf v2 para el flujo online y persiste:

- la raíz bootstrap fijada y la cadena completa de rotaciones verificadas;
- metadata timestamp, snapshot, targets y roles delegados recorridos;
- un manifiesto SHA-256, high-water por rol y una generación activa;
- las relaciones entre roles delegados y sus delegadores.

El staging aislado permite rehidratar una generación y volver a validarla durante un refresh online. No es todavía una verificación offline.

El modo `UnsafeLocalMode` de go-tuf no será utilizado como atajo de producción. Aunque permite operar sin interacción de red con metadata local, SOPTRAZ necesita controlar explícitamente la raíz bootstrap, el replay de su cadena, la elegibilidad de la generación activa, la expiración, el estado anti-rollback, los límites y la resolución de delegaciones. La documentación de go-tuf describe `TrustedMetadata` como la abstracción que mantiene una colección de metadata confiable durante el flujo TUF; el updater oficial utiliza esa misma capa para validar roles. Véase [go-tuf v2.5.0 updater](https://github.com/theupdateframework/go-tuf/blob/v2.5.0/metadata/updater/updater.go) y [TUF Specification](https://theupdateframework.github.io/specification/latest/).

La sesión `MetadataSession` existente valida una secuencia top-level (timestamp, snapshot y targets) y permite consultar targets top-level. No cubre el grafo de delegaciones persistido ni debe presentarse como un verificador offline completo.

## Decisión

Crear un `OfflineCatalogVerifier` explícito dentro de `internal/modules/catalog`, basado en `trustedmetadata` oficial y en una generación SQLite previamente comprometida. Será una operación local, read-only respecto de los artefactos y sin transporte de red.

No se creará un segundo formato de firmas, un verificador criptográfico propio ni una ruta de ejecución alternativa.

### 1. Entrada y resultado

La API recibirá, como mínimo:

- un `TrustAnchor` construido desde un root cuyo SHA-256 esté fijado por el binario o por un enrolamiento independiente ya verificado;
- el almacén de confianza SQLite y el ID del repositorio TUF configurado;
- un descriptor artifact v1 que se desea validar;
- límites de delegación definidos por configuración confiable de SOPTRAZ;
- evidencia temporal obtenida mediante el `TrustedTimeProvider`, con procedencia, incertidumbre y nivel de assurance según ADR-0020. El reloj de pared del sistema sigue siendo una fuente operativa, pero no se confunde con tiempo autenticado.

El resultado será un `VerifiedTarget` junto con evidencia de la validación: repositorio, generación, root version/hash, roles realmente recorridos, fecha/hora de referencia, expiración más próxima y nivel de assurance local.

El resultado **no** es autorización para ejecutar. Tampoco descarga el target ni confirma que el archivo instalado exista: el resolver local de artefactos verifica por separado ruta confinada, plataforma, tamaño y SHA-256. M1.5 sigue siendo requisito para cualquier ejecución externa.

### 2. Snapshot consistente de SQLite y exclusión mutua

El verificador adquiere el mismo lock de repositorio que serializa los clientes TUF online. No puede validar una mezcla de una root-chain leída antes de una actualización y una generación leída después.

El almacén agregará una operación de lectura consistente (por ejemplo, `LoadOfflineSnapshot`) que obtenga, dentro de una única transacción SQLite de lectura:

- configuración y pin bootstrap registrados para el repositorio;
- cadena root completa y latest-root version/hash;
- generación activa, todos sus blobs, relaciones, high-water y manifiesto;
- estado estructurado de evidencia temporal confiable y último instante atestiguado aceptado, cuando exista.

La lectura valida integridad estructural, tamaños, digests, manifiesto, referencias y high-water. Ningún resultado de SQLite se considera autenticado criptográficamente hasta que el catalog replay complete.

Una generación cuyo `RootState` no sea exactamente igual a la última raíz verificada se rechaza. No se recupera una generación anterior para mejorar disponibilidad.

### 3. Reconstrucción de la raíz confiable

- Empezar siempre desde el root bootstrap original y su pin independiente, no desde el root final almacenado.
- Reproducir cada rotación de forma consecutiva con `trustedmetadata.UpdateRoot`, verificando cada transición mediante go-tuf.
- Rechazar huecos de versión, firma/threshold inválidos, cadena alterada, raíz equivoca o divergencia entre la última raíz reproducida y el high-water local.
- Exigir que el root final coincida byte por byte (versión y SHA-256) con el root de la generación activa.
- Comprobar la expiración del root con la misma hora de referencia que se utilizará en timestamp, snapshot y targets.

No se acepta un root descargado o contenido en la caché como bootstrap de confianza.

### 4. Replay de la generación

Crear un `trustedmetadata.TrustedMetadata` nuevo y aislado desde el root final reproducido. Fijar una única `RefTime` para toda la validación; no consultar el reloj por separado entre roles.

Revalidar, en orden:

1. `timestamp`: firma, expiración, versión y límites;
2. `snapshot`: firma, expiración, versión y consistencia con timestamp;
3. `targets` top-level: firma, expiración, versión y consistencia con snapshot;
4. los roles delegados necesarios: firma del rol padre, autorización, versión, expiración, hash/tamaño declarados por snapshot y relación de delegación.

Los bytes originales son los almacenados en la generación. La metadata no se vuelve a serializar antes de verificar firmas o hashes.

Se reutilizará y refactorizará el replay ya presente en `captureAndVerifyGenerationMetadata` para evitar que el flujo online y el offline desarrollen algoritmos de validación divergentes. La persistencia solo almacena bytes y relaciones; el paquete `catalog` sigue siendo responsable de autenticarlos.

### 5. Resolución correcta de delegaciones

Para el `RelativePath` del descriptor:

- reproducir el recorrido depth-first pre-order definido por TUF, respetando el orden firmado de las delegaciones;
- respetar patrones de rutas, prioridades, roles terminating y máximo de delegaciones;
- verificar cada rol recorrido con el padre autorizado antes de inspeccionar sus targets;
- resolver solo con roles incluidos en la generación activa y criptográficamente reproducidos;
- si un rol candidato requerido no está en la generación, está expirado o no puede validarse, devolver `offline target unavailable` y fallar cerrado. No saltar silenciosamente al siguiente rol: podría existir allí un target de mayor prioridad;
- detectar ciclos y limitar la cantidad/tamaño de metadata igual que en el flujo online.

La generación actual contiene solo los roles que fueron recorridos por descriptores resueltos previamente; no se afirma que incluya todo el grafo del repositorio. Por tanto, el modo offline solo podrá validar targets cuyo recorrido completo esté disponible en esa generación.

El algoritmo de recorrido debe compartirse con la ruta de captura online o verificarse mediante fixtures equivalentes. No crear una interpretación distinta del orden de delegaciones.

### 6. Descriptor artifact v1

El target seleccionado debe contener el descriptor en `custom`, autenticado por el rol que lo publica. Aplicar el decoder JSON estricto existente, rechazar claves duplicadas, escapes Unicode inválidos y campos desconocidos, y exigir igualdad completa entre descriptor solicitado y descriptor firmado.

También deben coincidir exactamente:

- `relativePath`;
- `sizeBytes` con la longitud TUF;
- `sha256` del descriptor con el hash TUF;
- versión, SO, arquitectura, licencia y procedencia según el contrato artifact v1.

El verificador no convierte un descriptor provisto por la UI o por IA en una fuente de confianza. Solo lo compara con metadata que ya superó el replay criptográfico.

### 7. Expiración, reloj y nivel de assurance

La política temporal de esta arquitectura se define y queda supersedida por [ADR-0020 — Fuente de tiempo confiable y recuperación ante relojes incorrectos](ADR-0020-trusted-time-and-clock-recovery.md).

Se mantienen estas invariantes del verificador offline:

- Todos los roles requeridos deben estar vigentes en una misma referencia temporal por operación.
- La referencia debe proceder de evidencia temporal admitida por la política de assurance; un timestamp SQLite generado con el reloj local no constituye por sí solo un reloj seguro.
- Una expiración nunca se extiende para compensar un reloj incorrecto. Cuando exista incertidumbre temporal, la comprobación usa el extremo superior conservador del intervalo.
- Una anomalía del reloj local no invalida automáticamente el repositorio ni bloquea globalmente SOPTRAZ. La recuperación online intenta restablecer hora confiable; offline falla con `TrustedTimeUnavailable` cuando no puede demostrar vigencia.
- La migración SQLite v2→v3 no debe convertir `created_at`, `committed_at` ni `activated_at` históricos en un piso temporal confiable. El estado temporal se guarda con procedencia, y puede quedar vacío hasta obtener la primera evidencia aceptada.
- No se usa `UnsafeSetRefTime` de go-tuf en producción. Hasta disponer de una API soportada para inyectar hora, el updater oficial requiere que el reloj efectivo del sistema haya sido corregido por el mecanismo temporal configurado.

El nivel de assurance y las condiciones de disponibilidad offline se informan por separado del resultado criptográfico TUF. La restauración integral de una imagen antigua del mismo disco sigue siendo una limitación cuando no existe contador hardware monotónico o witness externo.

### 8. Límites operativos

- Cero HTTP, DNS, sockets o fallback a red dentro de `OfflineCatalogVerifier`.
- Cero `UnsafeLocalMode`.
- No reconstruir metadata desde un descriptor aislado ni aceptar TOFU.
- No aceptar generaciones inactivas, antiguas, incompletas, con root distinto o con metadata faltante.
- No leer ejecutables desde `PATH`, descargar, instalar, extraer ni lanzar procesos.
- No alterar el target instalado ni su directorio; la verificación del archivo queda en el resolver.
- Cualquier fallo produce una causa tipada y una capacidad offline no disponible; no cambia silenciosamente el estado de confianza.
- La IA/UI no puede elevar límites, escoger otro repositorio o desactivar controles.

## Consecuencias

**Beneficios**

- El offline se apoya en el mismo root, reglas y metadata TUF que el flujo online.
- Se evita depender del comportamiento especial de `UnsafeLocalMode`.
- Las delegaciones conservan prioridad y terminating; no se aceptan targets de roles no cargados.
- La generación comprometida sigue siendo el registro autoritativo y se puede reconstruir una caché de lectura sin confiar en el directorio temporal.
- El límite anti-rollback local se declara sin fingir protección hardware.

**Costos**

- Hay que refactorizar el replay existente para reutilizarlo sin duplicar la autoridad de validación.
- SQLite necesitará un estado temporal v3 con procedencia/evidencia y una lectura snapshot que combine root-chain, high-water, generación y estado temporal; el backfill de fechas locales antiguas no será un ancla confiable.
- Offline no puede validar un target cuyo recorrido delegado no esté incluido en la generación actual.
- El estado de reloj local puede requerir una recuperación explícita tras una corrección manual importante del reloj.

## Plan de implementación y aceptación

1. Definir e implementar `TrustedTimeProvider`, el estado persistente de evidencia temporal y tests de contrato/migración v2→v3 sin convertir fechas locales antiguas en anclas confiables.
2. Extraer un único replay reusable que valide root-chain y una generación desde bytes exactos con hora de referencia explícita.
3. Implementar el resolver offline de delegaciones, reusando el orden y límites del recorrido online.
4. Vincular descriptor artifact v1 con la misma función canónica de validación; no duplicar reglas JSON ni hashes.
5. Probar offline exitoso para un target top-level y uno delegado después de cerrar el cliente online y bloquear toda red.
6. Probar fallos por root pin incorrecto, cadena no consecutiva, high-water, generación/root mismatch, rollback/equivocation, timestamp/snapshot/targets/delegación alterados, descriptor discrepante, expiración límite, evidencia temporal atrasada/contradictoria/ausente, rol ausente, ciclo y exceso de delegaciones.
7. Probar que un delegado candidato ausente no se salta en favor de una delegación posterior.
8. Demostrar con un test explícito que restaurar una copia antigua integral del mismo SQLite no se detecta sin ancla independiente; documentarlo como limitación, no como éxito de seguridad.
9. Ejecutar Windows, macOS y Ubuntu, Race Detector, fuzzing acotado, fallos SQLite e integración sin red.
10. Verificar que no se ejecuta ningún artefacto: M1.5 conserva esa autorización y es una puerta separada.

M1.4 no se cierra hasta que la verificación online, la persistencia, la política de confianza, este replay offline y su matriz de pruebas estén aceptados. La firma/bootstrap de producción y la revisión SBOM siguen siendo entregas separadas.
