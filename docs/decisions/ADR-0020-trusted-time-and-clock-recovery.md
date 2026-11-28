# ADR-0020: Fuente de tiempo confiable y recuperación ante relojes incorrectos

- Estado: Aceptada como política de arquitectura; contrato puro y política de aceptación en memoria implementados; POC Roughtime RFC 10049 v1 strict aislada presente en PR #31; NTS, bootstrap de frescura, gobernanza de claves, persistencia anti-rollback e integración TUF pendientes.
- Fecha: 2026-11-28
- Relacionada: ADR-0015, ADR-0017, ADR-0019, Issue #5 (M1.4), Issue #4 (M1.5).
- Alcance: establecer la hora de referencia para verificar expiración y frescura de metadata TUF. No cambia la firma de artefactos ni autoriza su ejecución.

## Contexto

La expiración de TUF es una defensa contra ataques de freeze: un repositorio o atacante no debe poder mantener indefinidamente metadata antigua en un cliente desconectado. Esa defensa depende de que el cliente tenga una referencia temporal razonablemente correcta.

TUF no sincroniza el reloj. La especificación 1.0.37 (8 de octubre de 2026) mantiene *Support for bogus clocks* entre sus preguntas abiertas: reconoce que muchas máquinas tienen relojes incorrectos y que puede ser necesario un modo explícito para esos entornos. El workflow fija una sola hora al comienzo de cada actualización para que las comprobaciones de expiración sean coherentes durante ese ciclo; no proporciona una fuente de tiempo confiable ni resuelve por sí mismo un reloj adelantado o atrasado.

La respuesta de un mantenedor de TUF en el foro oficial es explícita: TUF delega la sincronización al entorno de ejecución y cita NTP, Roughtime y el enfoque de Uptane para dispositivos que no disponen de RTC. Véase [Some questions about TUF — Google Groups, 7 de abril de 2020](https://groups.google.com/g/theupdateframework/c/OO6ClFXyBIA).

Uptane, el estándar de actualizaciones de software automotriz basado en TUF, va más allá: exige una fuente de tiempo segura o una atestación suficientemente reciente, conserva el último tiempo autenticado, no acepta retrocesos y define cómo rotar las claves de la fuente temporal. Sus recomendaciones consideran NTP con NTS, Roughtime y fuentes externas autenticadas. Véanse [Uptane Standard 2.1.0](https://uptane.org/docs/latest/standard/uptane-standard) y [Uptane Deployment Best Practices 2.0.0, sección 3.1](https://uptane.org/docs/2.0.0/deployment/best-practices).

La política anterior de SOPTRAZ proponía bloquear **toda** validación online y offline cuando el UTC local fuese anterior a `last_observed_utc`. Esa política mezcla tres conceptos diferentes:

1. hora de pared del sistema operativo, que puede corregirse, adelantarse o atrasarse;
2. evidencia de una fuente temporal autenticada, con una identidad y un grado de incertidumbre;
3. memoria anti-rollback del catálogo, que debe conservarse aunque cambie el reloj local.

Además, si una hora incorrecta adelantada se persiste como piso confiable, una corrección posterior del reloj puede dejar al catálogo bloqueado aunque la hora actual ya sea correcta. Una marca SQLite escrita usando `time.Now()` no se convierte en un reloj seguro por almacenarla de forma transaccional.

## Decisión

### 1. Se elimina el bloqueo global por comparación con el reloj local

Queda supersedida la regla de ADR-0019 que rechazaba automáticamente online y offline cuando `time.Now().UTC() < last_observed_utc`.

Una discrepancia del reloj local se tratará como **anomalía temporal recuperable**, no como prueba concluyente de que el repositorio o su metadata son inválidos.

- Un reloj atrasado no debe impedir que SOPTRAZ intente recuperar hora confiable cuando existe conectividad.
- Una validación offline no debe presentar metadata como vigente si no puede establecer una hora de referencia suficientemente confiable.
- La anomalía de hora no debe cerrar ni inutilizar el resto de la aplicación. Solo se bloquea la operación cuya seguridad depende de comprobar la expiración.
- Nunca se desactivan las comprobaciones de expiración para recuperar disponibilidad. No se utiliza `UnsafeLocalMode`, ni un modo equivalente de “aceptar aunque el reloj esté mal”.

### 2. Separar hora local, evidencia temporal y memoria anti-rollback

El modelo de M1.4 distinguirá:

- **SystemWallClock:** hora UTC devuelta por Windows, macOS o Linux. Es útil para operación y diagnóstico. El estado “sincronizado” informado por el sistema operativo es una señal operativa, pero no equivale por sí sola a una atestación criptográfica independiente.
- **TrustedTimeEvidence:** muestra de una fuente temporal aceptada por la política de SOPTRAZ. Incluye el instante o intervalo UTC, incertidumbre declarada, clase e identidad de la fuente y, cuando corresponda, identidad de clave, material de verificación o digest de la atestación.
- **LastAcceptedTrustedTime:** última evidencia temporal aceptada, persistida monotónicamente con su procedencia. No se actualiza por crear una fila, escribir un log, activar una generación o llamar a `time.Now()`.

Una muestra temporal debe poder expresar un intervalo `[earliest, latest]`, no fingir precisión absoluta. Para verificar expiración, el límite superior conservador es el que manda: metadata con `expires <= latest` se rechaza. La incertidumbre de la fuente no se transforma en un margen que extienda la expiración.

La política de aceptación debe impedir que una muestra autenticada haga retroceder el último tiempo aceptado. La comparación contemplará el intervalo y su incertidumbre; no se fijará una tolerancia arbitraria en minutos. La precisión permitida depende del método y sus garantías documentadas.

### 3. Fuentes de tiempo y niveles de assurance

La interfaz interna futura será un `TrustedTimeProvider` que entregue una muestra con procedencia y límites explícitos. Se contemplan los siguientes niveles:

| Clase | Ejemplos | Garantía y uso |
|---|---|---|
| `system` | Reloj del sistema operativo; estado de sincronización informado por el servicio nativo | Compatibilidad con el modelo habitual de TUF. No se presenta como fuente criptográficamente independiente. |
| `authenticated` | NTP con NTS, atestación firmada de un servidor de tiempo, Roughtime con claves fijadas y política de múltiples fuentes | Permite recuperar una hora cuando el reloj local es incorrecto; depende de la confianza, independencia, rotación y seguridad de las fuentes configuradas. |
| `hardware` | Reloj o contador persistente protegido por hardware/plataforma, solo cuando haya soporte y pruebas reales | Puede aportar continuidad temporal/monotónica entre reinicios, según las garantías específicas del dispositivo. No se presume disponible. |

Referencias y límites:

- **NTS, RFC 8915 (Standards Track):** autentica NTP mediante TLS y AEAD. Protege el canal frente a alteraciones de terceros, pero la autenticidad de una respuesta no demuestra que un único servidor esté libre de compromiso.
- **NTP BCP 223, RFC 8633:** recomienda al menos cuatro fuentes independientes y diversas cuando se busca conservar precisión, y advierte que varias fuentes pueden compartir causas de error o verse afectadas por una red comprometida. Este número es una recomendación operativa para NTP, no un quorum criptográfico universal para SOPTRAZ; la independencia real de las fuentes debe comprobarse.
- **Roughtime, RFC 10049 (Experimental, octubre de 2026):** usa claves de largo plazo fijadas, un nonce fresco y una respuesta firmada con un intervalo de incertidumbre. El modo multi-servidor permite detectar inconsistencias entre fuentes; su ecosistema continúa siendo experimental y no se asumirá disponible en todos los sistemas.
- **Khronos, RFC 9523 (Informational, febrero de 2024):** propone selección y filtrado distribuido de muestras de muchos servidores NTP para resistir ataques de desplazamiento temporal. Es una referencia de investigación/industria, no una dependencia inicial de SOPTRAZ.
- **NTP sin autenticación** puede servir a la sincronización ordinaria del sistema, pero no se tratará como atestación criptográfica propia de SOPTRAZ.

La política concreta de fuentes, independencia, cantidad mínima de respuestas coincidentes, dispersión geográfica, incertidumbre máxima y rotación de claves debe quedar definida y probada antes de calificar una muestra como `authenticated`. No se fijará un quorum numérico sin especificar esas condiciones.

### Primera implementación de la política pura de aceptación

El paquete `internal/platform/trustedtime` incorpora `AcceptancePolicy` y `AcceptedTime` como una primera capa determinista, sin I/O:

- La allowlist compara exactamente el `Source` provisionado: ID, operador, tipo, assurance, trust anchor y KeyID. El proveedor no puede autodeclarar una identidad distinta.
- Solo se aceptan muestras `authenticated` o `attested`. Las señales `system` se conservan para diagnóstico y no pueden establecer esta política.
- Cada lote cuenta como máximo una observación por OperatorID. Un segundo hostname, dirección o SourceID del mismo operador no suma independencia.
- Se validan límites máximos de incertidumbre por muestra y sobre la intersección final.
- Se selecciona el grupo de intervalos con mayor intersección común. Solo se acepta cuando ese grupo máximo es único y alcanza `MinDistinctOperators`; los grupos máximos empatados y disjuntos se rechazan. No se promedian tiempos ni se elige una rama de forma arbitraria.
- El último límite inferior previamente aceptado actúa como piso monotónico: si el nuevo intervalo termina antes, se rechaza; si cruza el piso, se recorta sin ampliar su límite superior.
- El resultado conserva las evidencias y los digest de respuesta de cada fuente seleccionada, además del intervalo conservador.

Esta función **no** verifica firmas, anclas, nonces, frescura ni replay. Tampoco decide si los operadores son realmente independientes: esa relación debe venir de la configuración de confianza. Cada adaptador debe autenticar el protocolo antes de devolver Evidence. La política no lee `time.Now()`, no persiste y no corrige el reloj del sistema.

La implementación y sus límites están descritos en [trusted-time-provider.md](../architecture/trusted-time-provider.md). Las pruebas cubren fuentes no autorizadas, identidad alterada, duplicidad de operador, intervalos disjuntos o ambiguos, outlier, límites de incertidumbre, NTS/Roughtime, piso anti-rollback y lotes sobredimensionados.


### Bootstrap temporal: resolver la dependencia circular de NTS

NTS-KE autentica su servidor con TLS y certificados X.509. La validación normal de `notBefore` y `notAfter` necesita una hora aproximadamente correcta; por eso NTS por sí solo no resuelve de forma limpia el primer contacto cuando el reloj carece de referencia. El RFC 8915 reconoce expresamente este problema y propone mitigaciones, sin afirmar que exista una solución perfecta.

SOPTRAZ adoptará una secuencia escalonada:

1. **Primera opción: hora segura ya disponible.** Usar RTC funcional y estado de sincronización del sistema operativo. Si el sistema informa que una fuente confiable ya disciplinó el reloj, validar certificados NTS normalmente.
2. **Recuperación cuando no hay hora correcta: fuente de bootstrap con claves fijadas.** Evaluar Roughtime con un conjunto de claves públicas provisionadas por un canal independiente de la conexión consultada. Verificar nonce, firma, delegación/certificado del servidor, intervalo de incertidumbre y respuestas inconsistentes entre servidores. La hora obtenida se usa primero como evidencia aproximada para recuperar el reloj del sistema; no se convierte automáticamente en una autorización para aceptar metadata TUF.
3. **Sincronización sostenida: NTS.** Una vez que la hora del sistema permite validar el certificado TLS del servidor NTS, usar NTS para la disciplina continua del reloj. SOPTRAZ verifica que la respuesta obtenida sea consistente con la validez del certificado NTS y con la última evidencia temporal aceptada.
4. **Sin internet o sin fuentes autenticadas:** no inventar una hora ni aceptar certificados por una excepción genérica. Se permite un procedimiento administrativo explícito para corregir aproximadamente el reloj, pero el estado de assurance será `system` hasta que exista evidencia autenticada. Para operaciones que requieran assurance más alto, se mantiene el bloqueo específico de la validación.

RFC 10049 describe Roughtime precisamente para dispositivos sin una idea inicial de la hora y explica que sus claves públicas de largo plazo funcionan como raíces de confianza. Sin embargo, es una RFC Experimental y no define por sí sola el ecosistema de publicación, gobernanza y rotación de listas de servidores. Por eso SOPTRAZ lo considera una opción de recuperación piloto detrás de una interfaz, no un requisito obligatorio ni una dependencia de producción inicial. La lista de servidores y claves deberá venir fijada en una versión firmada de SOPTRAZ o actualizarse mediante un canal independiente autenticado; nunca se aceptará una clave entregada por el mismo servidor cuya hora se está verificando. La firma de una respuesta Roughtime demuestra que el servidor autenticado firmó ese valor dentro del intervalo declarado; no demuestra por sí sola que el valor sea correcto. Un servidor comprometido o mal configurado aún puede firmar una hora falsa. La independencia de operadores/infraestructura y la comparación de respuestas son parte de la política de confianza, no una propiedad mágica de la firma.

Si el administrador decide corregir manualmente el reloj, la acción debe mostrar la diferencia detectada, fuente usada, incertidumbre y consecuencia. No se cambia el reloj global automáticamente sin permiso del usuario/sistema operativo.

### 4. Recuperación online ante una anomalía

Cuando SOPTRAZ detecte que el reloj local está atrasado, demasiado adelantado o presenta una discontinuidad frente a la última evidencia aceptada:

1. clasifica la condición como `ClockAnomaly`; no reduce ni sobrescribe `LastAcceptedTrustedTime`;
2. consulta la disponibilidad y estado del proveedor temporal configurado, incluyendo el servicio de hora del sistema y, cuando esté configurado, una fuente autenticada independiente;
3. verifica identidad, autenticidad, frescura, incertidumbre y no-regresión de la nueva evidencia;
4. solicita la resincronización normal del sistema operativo o espera a que la fuente que gobierna su reloj corrija la hora;
5. solo cuando la hora efectiva usada por go-tuf coincide con la referencia temporal aceptada dentro de la incertidumbre permitida, inicia el workflow TUF con una única hora fija;
6. después de completar el replay y el commit de una generación, persiste la nueva evidencia temporal y la generación mediante la política transaccional definida para M1.4.

Si la recuperación temporal no está disponible o falla, la actualización TUF falla cerrada con un error tipado de tiempo no confiable. Esto no impide diagnósticos locales ni otras funciones que no dependan de metadata con expiración.

**Restricción de go-tuf:** la API pública actual de `updater.Updater` documenta `UnsafeSetRefTime` como exclusiva para pruebas. SOPTRAZ no la usará en producción ni modificará el reloj global del sistema por su cuenta. La primera integración debe apoyarse en una hora de sistema corregida por el servicio temporal elegido. Si se requiere pasar una atestación directamente al updater sin modificar el reloj del SO, habrá que solicitar una API segura de referencia temporal al proyecto go-tuf o mantener una extensión pequeña, revisada y probada; no se improvisará un bypass.

### 5. Política offline

La validación offline seguirá exigiendo metadata íntegra, firmas válidas, delegaciones correctas y expiración vigente. La diferencia es que la decisión temporal requiere evidencia apropiada:

- Una atestación almacenada puede proyectarse hacia el presente solo cuando existe una fuente monotónica confiable para medir el tiempo transcurrido y se conoce su límite de deriva.
- La continuidad de un reloj monotónico dentro del mismo arranque puede aportar evidencia limitada; tras un reinicio su contador puede reiniciarse y no demuestra cuánto tiempo real transcurrió.
- Para aceptar offline después de reiniciar se requiere reloj/contador persistente confiable, una nueva atestación o una fuente de tiempo equivalente cuya garantía haya sido comprobada.
- Un `last_observed_utc` guardado en la misma base SQLite y el reloj de pared actual no bastan para demostrar vigencia después de una restauración o retroceso.
- Si no existe evidencia temporal suficiente, el resultado es `TrustedTimeUnavailable` y no se devuelve `VerifiedTarget`. El catálogo se declara temporalmente no disponible; el resto de SOPTRAZ sigue operativo.

Por lo tanto, no se promete verificación offline indefinida después de apagar el equipo. Sin RTC/contador seguro o una atestación reciente, es imposible demostrar con rigor que una metadata no expiró durante el período desconectado.

### Offline estricto, verificación histórica y recuperación online

Las discusiones de mantenedores de TUF distinguen dos usos diferentes que suelen llamarse "offline":

1. **Offline estricto:** no se consulta el repositorio remoto, pero toda metadata local debe seguir vigente en el momento de la verificación. Se conserva la protección de expiración; lo único que se omite es el refresh remoto.
2. **Verificación histórica con metadata expirada:** se comprueba la firma/cadena local aunque la metadata ya no esté vigente ahora. Esto sacrifica frescura y puede aceptar claves o artefactos que desde entonces fueron revocados o reemplazados. No equivale a un catálogo actual confiable.

La comunidad discutió el segundo comportamiento en python-tuf para Sigstore. El PR #2472 del mantenedor `jku` implementaba una opción que desactivaba expiry checks y evitaba descargas, pero quedó cerrado sin integrarse. En la discusión se señaló que aceptar metadata vencida puede tener poco beneficio respecto de confiar directamente en una caché local de targets cuando la metadata local no es más confiable que esa caché; para que TUF aporte protección real se necesita también verificar la metadata local desde el root bootstrap y proteger su almacenamiento. El mantenedor también describió un posible "PessimisticUpdater": intentar offline estricto y, si faltan datos o están vencidos, reiniciar la operación completa online. Véanse [Offline TUF — jku](https://gist.github.com/jku/71d6feaacb6a0b4f81a393a41ff3522d), [python-tuf #2359](https://github.com/theupdateframework/python-tuf/issues/2359) y [python-tuf PR #2472](https://github.com/theupdateframework/python-tuf/pull/2472).

**Decisión para SOPTRAZ:**

- `OfflineCatalogVerifier` solo implementará offline estricto: sin red, con root bootstrap, generación y delegaciones revalidadas, tiempo suficiente y toda metadata vigente.
- La verificación histórica de metadata expirada, si algún día se necesita para auditoría forense, será una operación separada de solo lectura. Deberá informar explícitamente que la evidencia fue válida en un intervalo pasado, no que el target esté actualmente autorizado. No devolverá `VerifiedTarget` ni habilitará descarga, instalación o ejecución.
- La capa de aplicación podrá hacer un intento optimista offline. Si falla por expiración, evidencia temporal insuficiente o metadata/delegación ausente, descartará toda la sesión y cualquier resultado parcial. Si existe conectividad, solicitará recuperación temporal si corresponde, creará una sesión/updater online nuevo y repetirá desde el comienzo la resolución completa del descriptor y su cadena de delegación.
- No se mezclará metadata offline vieja con metadata online nueva, ni se conservará un `VerifiedTarget` de la primera tentativa mientras se actualiza otra parte del grafo. El target solo se devuelve después de una validación completa y un commit durable de la generación vigente.
- Si no hay conectividad o la recuperación temporal falla, el catálogo queda no disponible para esa operación. No se extiende `expires`, no se usa un `now` histórico y no se omite expiry.

Esta estrategia mejora disponibilidad cuando el equipo está conectado pero la caché venció o el reloj necesita corregirse, manteniendo una única decisión de confianza para el target final.


### 6. Rotación y compromiso de las claves temporales

Las claves que autoricen fuentes temporales deberán quedar ancladas en una raíz de confianza independiente y rotarse mediante un procedimiento autenticado. Una respuesta de tiempo adelantada podría causar denegación de servicio; una respuesta atrasada podría habilitar un freeze. La rotación de la clave de tiempo no se tratará como una corrección ordinaria del reloj.

Un eventual reinicio/rebase del piso temporal solo será posible después de verificar la rotación de la clave fuente bajo la raíz confiable y obtener una nueva evidencia aceptable. El procedimiento será explícito, auditable y tendrá pruebas contra replay. Nunca se rebajará el piso por una opción de UI, una variable de entorno o una preferencia no autenticada.

### 7. Migración SQLite v2→v3

La migración no debe convertir fechas de pared antiguas en evidencia temporal confiable:

- El esquema v3 agregará el estado estructurado de tiempo confiable por repositorio, con procedencia y evidencia suficiente para reconstruir la decisión.
- Las columnas `created_at`, `committed_at` y `activated_at` de v2 fueron registradas con la hora local del proceso. No son prueba de tiempo autenticado y **no se usarán para inicializar un piso de confianza**.
- Si se conservan fechas históricas para diagnóstico, deberán quedar marcadas explícitamente como `system/local-untrusted`, sin participar en la aceptación de expiración.
- El estado `LastAcceptedTrustedTime` podrá quedar vacío después de migrar una base v2. Se inicializará solo con la primera muestra que cumpla la política de fuente y assurance seleccionada.
- La migración será atómica, reversible ante errores y tendrá fixtures v2 con repositorio, roots y generaciones. No se consultará `time.Now()` para inventar un valor histórico.

### 8. Pruebas y criterios obligatorios

1. Reloj local atrasado con fuente autenticada disponible: recuperar hora, iniciar refresh TUF y no aceptar metadata expirada.
2. Reloj local adelantado: no envenenar el estado persistido con esa hora; corregir mediante fuente confiable y repetir verificación.
3. Fuente temporal con firma inválida, clave desconocida, respuesta repetida, nonce incorrecto, incertidumbre excesiva o tiempo anterior al último aceptado: rechazar evidencia y conservar el estado previo.
4. Dos o más fuentes que no concuerdan: no elegir silenciosamente una; producir error tipado y diagnóstico sin filtrar secretos.
5. Bootstrap sin hora válida: comprobar recuperación con claves Roughtime fijadas para luego validar TLS/NTS, sin desactivar globalmente la validación X.509.
6. NTS-KE con reloj incorrecto: probar certificados `notBefore`/`notAfter` fuera del reloj local, con y sin evidencia previa de sincronización; conservar validación estricta cuando ya existe estado temporal confiable.
7. Metadata cuya expiración cae dentro del intervalo de incertidumbre: rechazarla.
8. Sin red y mismo arranque: probar cálculo desde tiempo autenticado más reloj monotónico, límite de deriva y límite máximo de antigüedad.
9. Sin red después de reiniciar, sin RTC/contador confiable: devolver `TrustedTimeUnavailable`, nunca un target.
10. Rotación y compromiso de la clave temporal, recuperación del piso y replay de atestaciones antiguas.
11. Migración v2→v3: demostrar que timestamps locales antiguos no se convierten en anclas confiables.
12. Windows, macOS y Ubuntu; Race Detector, fuzzing de parser/atestaciones y tests de concurrencia.
13. Confirmar que el updater usa una sola referencia temporal por ciclo y que SOPTRAZ nunca llama a `UnsafeSetRefTime` en producción.
14. Offline estricto fallido por expiración o delegación faltante: descartar toda la sesión y repetir online desde cero; comprobar que nunca se devuelve ni se conserva un `VerifiedTarget` parcial de la tentativa offline.
15. Verificación histórica, si se implementa en el futuro, no debe producir un target apto para instalación/ejecución ni modificar el catálogo activo.

## Alternativas consideradas

- **Bloquear todo online y offline si el UTC local es anterior al último instante SQLite:** descartada como política general. Provoca bloqueos persistentes por correcciones normales del reloj y no ofrece un mecanismo de recuperación. La memoria local no sustituye a una fuente de tiempo confiable.
- **Permitir siempre una tolerancia fija de reloj:** descartada. Una ventana arbitraria puede ampliar la vida efectiva de metadata expirada. Solo se acepta la incertidumbre que forme parte de la evidencia temporal verificada.
- **Desactivar comprobaciones de expiración cuando el reloj es incorrecto:** descartada. APT permite deshabilitar comprobaciones de fecha por repositorio/globalmente para usos especiales, pero su documentación advierte que eso elimina tanto `Check-Valid-Until` como los controles de fechas futuras. SOPTRAZ no adoptará ese bypass para su catálogo.
- **Confiar ciegamente en un servidor NTP:** descartada. NTS autentica la comunicación, no la honradez de un único servidor; la política de fuentes debe considerar compromiso e inconsistencias.
- **Usar `UnsafeSetRefTime` de go-tuf en producción:** descartada porque la propia API la limita a pruebas.

## Consecuencias

**Beneficios**
- Una corrección del reloj no bloquea automáticamente la recuperación online.
- La expiración sigue siendo obligatoria y se evalúa conservadoramente.
- SOPTRAZ distingue hora del sistema de evidencia temporal autenticada.
- La limitación offline tras reinicios sin fuente segura queda declarada en vez de ocultarse.

**Costos**
- Se necesita una abstracción `TrustedTimeProvider`, diagnóstico de la sincronización del SO y una política explícita de fuentes.
- El estado SQLite v3 guarda procedencia/evidencia temporal, no solo un texto UTC.
- La integración online directa de un tiempo autenticado requerirá una API soportada por go-tuf o que el sistema operativo esté corregido por un servicio temporal confiable.
- La verificación offline no siempre estará disponible en máquinas sin reloj/contador seguro y desconectadas tras reiniciar.

## Evaluación de candidatos de implementación (28-11-2026)

Esta evaluación orienta una futura prueba de concepto. **No selecciona ni incorpora todavía una dependencia de producción.** La existencia de una implementación cliente no sustituye la revisión de criptografía, mantenimiento, interoperabilidad, fuentes, licencias y recuperación.

### NTS en Go

- `github.com/beevik/nts` implementa un cliente NTS para Go, con sesión NTS-KE sobre TLS y consultas NTP protegidas; expone `Response.Validate()` y configuración de timeout/dialer/resolver. La ficha consultada muestra `v0.3.3`, BSD-2-Clause y siete módulos importadores; la línea sigue en `v0.x`, por lo que no se trata como API estable ni como dependencia aceptada.
- La propia documentación del paquete indica que hay pocos servidores públicos NTS y que NTP Pool no lo soporta actualmente. Una sesión NTS requiere primero un handshake TLS válido: por tanto, el paquete **no resuelve por sí solo el arranque con reloj gravemente incorrecto**.
- Puede ser candidato a un adaptador NTS de laboratorio tras revisar el código de NTS-KE, manejo de cookies/nonce, validación de respuesta, incertidumbre y dispersiones reportadas, dependencias transitivas, fuzzing, compatibilidad Windows/macOS/Linux y comportamiento con certificados `notBefore`/ `notAfter` cuando el reloj está fuera de rango.
- La aplicación no utilizará `InsecureSkipVerify` ni un callback equivalente para aceptar certificados vencidos o todavía no válidos. Una excepción temporal de validación TLS debilitaría el canal que debe recuperar la hora.

Referencias: [paquete beevik/nts](https://pkg.go.dev/github.com/beevik/nts), [repositorio y documentación](https://github.com/beevik/nts).

### Roughtime en Go

- `github.com/cloudflare/roughtime` tiene licencia Apache-2.0; pkg.go.dev reporta 24 módulos importadores en la ficha consultada. Su documentación advierte que la API es inestable y que conserva compatibilidad con drafts del protocolo. El servicio público de Cloudflare está anunciado como beta y su clave pública puede cambiar. No se usará una clave recuperada dinámicamente por DNS como raíz de confianza.
- `github.com/tannerryan/roughtime` declara soporte para RFC 10049 v1, incluye cliente/protocolo/tests, licencia BSD-2-Clause y requiere Go 1.27.4 o posterior. La página del repositorio observada tenía 45 commits, 12 estrellas y 3 forks. Es una opción interesante para un laboratorio alineado con el RFC final, pero su tamaño/adopción públicos no acreditan por sí mismos madurez, revisión independiente ni operación sostenida.
- Para cualquier POC, las claves públicas se provisionarán de forma independiente y explícita; las consultas usarán nonce fresco y se comprobarán las firmas, el intervalo RADI/incertidumbre, vigencia, repetición y contradicciones entre operadores/endpoints. La firma acredita lo que el servidor afirmó, no que esa hora sea verdadera. La diversidad real de administración y de fuentes ascendentes debe revisarse; diferentes nombres DNS o IPs pueden compartir el mismo operador o reloj.
- Antes de utilizarlo en producción deben existir pruebas de interop RFC 10049 v1, vectores oficiales, parser fuzzing, timeouts/backoff, UDP/TCP fallback, protección contra respuesta replay, rotación y revocación de claves y procedimiento de actualizar la lista de servidores mediante un canal autenticado.

Referencias: [Cloudflare Roughtime Go](https://pkg.go.dev/github.com/cloudflare/roughtime), [estado y directrices de su ecosistema](https://github.com/cloudflare/roughtime), [Cloudflare Roughtime beta y claves](https://developers.cloudflare.com/time-services/roughtime/usage/), [implementación Go de RFC 10049](https://github.com/tannerryan/roughtime).

### Servicios de tiempo integrados en los sistemas operativos

- **Windows:** W32Time mantiene una jerarquía y selecciona muestras NTP. En un dominio AD DS, las respuestas del controlador de dominio pueden autenticarse con claves de sesión Kerberos; W32Time rechaza paquetes con firma incorrecta. Un equipo no unido a dominio normalmente usa `time.windows.com`; Microsoft indica que no puede garantizarse precisión cuando la conectividad es intermitente o inexistente. SOPTRAZ podrá informar fuente/estado del servicio, pero solo elevará assurance cuando pueda verificar el contexto autenticado concreto, no por el mero hecho de que W32Time esté activo.
- **Linux/systemd:** `systemd-timesyncd` guarda una marca local después de sincronizar y puede avanzar aproximadamente el reloj al reiniciar para mantener monotonicidad sin RTC con batería. Implementa SNTP y el servicio no espera necesariamente a que se alcance una fuente exacta antes de continuar el arranque. Es una buena mitigación de disponibilidad/retroceso burdo, no una atestación criptográfica ni prueba de tiempo verdadero. El marcador `time-sync.target` es distinto del simple `time-set.target`.
- **macOS:** SOPTRAZ debe consultar y reportar el estado del servicio nativo disponible en el sistema, pero no asumir que el servicio del SO proporciona NTS o una prueba criptográfica independiente en todas las versiones/configuraciones. La clasificación inicial será `system` salvo evidencia adicional comprobada.

Referencias: [Microsoft — Windows Time Service](https://learn.microsoft.com/en-us/windows-server/networking/windows-time-service/how-the-windows-time-service-works), [systemd-timesyncd](https://www.freedesktop.org/software/systemd/man/latest/systemd-timesyncd.service.html), [systemd time synchronization targets](https://www.freedesktop.org/software/systemd/man/latest/systemd.special.html).

### Selección de implementación

La evaluación y el estado de la POC están registrados en [docs/research/trusted-time-provider-poc.md](../research/trusted-time-provider-poc.md). El PR #31 incluye una implementación aislada Roughtime RFC 10049 v1 strict sobre `github.com/tannerryan/roughtime v1.28.4`, con clave, commit upstream y pruebas multiplataforma fijados. Esto no constituye aprobación de producción ni habilita el flujo TUF.

1. Mantener los proveedores del sistema operativo como clase `system`, solo para lectura/diagnóstico, sin cambiar el reloj ni elevar assurance por inferencia.
2. Mantener Roughtime como POC aislada. Sus dos pasadas, firmas, nonces, delegación MINT/MAXT y orden causal se prueban en servidores locales UDP/TCP; no se toma la coherencia MINT/MAXT como prueba de frescura absoluta.
3. Completar vectores oficiales RFC, interoperabilidad independiente, análisis de dependencia, provisión/rotación/revocación de raíces, independencia administrativa verificable y bootstrap de frescura antes de considerar producción.
4. Auditar `beevik/nts` en una POC posterior al bootstrap TLS; el primer contacto NTS no debe saltarse validación X.509.
5. No conectar Roughtime/NTS al binario principal ni al Updater TUF hasta completar política operativa, persistencia anti-rollback, referencia temporal consumible por go-tuf y pruebas offline/online dedicadas.
6. Ninguna prueba de laboratorio puede marcar M1.4 como cerrado. No se habilita offline ni se omite expiración.

## Referencias

- [TUF Specification 1.0.37 — fixed update time y sección 7.1 Support for bogus clocks](https://theupdateframework.github.io/specification/latest/).
- [The Update Framework — foro oficial: “Some questions about TUF” (respuesta de un mantenedor, 7 de abril de 2020)](https://groups.google.com/g/theupdateframework/c/OO6ClFXyBIA).
- [Uptane Standard 2.1.0](https://uptane.org/docs/latest/standard/uptane-standard).
- [Uptane Deployment Best Practices 2.0.0 — sección 3.1 Secure source of time y sección 4.2 Metadata expiration](https://uptane.org/docs/2.0.0/deployment/best-practices).
- [RFC 8915 — Network Time Security for NTP (Standards Track)](https://www.rfc-editor.org/rfc/rfc8915.html).
- [RFC 8633 — Network Time Protocol Best Current Practices (BCP 223)](https://datatracker.ietf.org/doc/html/rfc8633): diversidad de fuentes, umbrales de sincronización y actuación manual ante saltos grandes.
- [RFC 8915, sección 8.5 — Initial Verification of Server Certificates](https://www.rfc-editor.org/rfc/rfc8915.html#section-8.5): problema de bootstrap TLS con reloj incorrecto y mitigaciones.
- [RFC 10049 — Roughtime (Experimental, octubre de 2026)](https://www.rfc-editor.org/rfc/rfc10049.html).
- [RFC 9523 — Khronos (Informational, febrero de 2024)](https://www.rfc-editor.org/rfc/rfc9523.html).
- [APT apt.conf(5) — Check-Date y Check-Valid-Until](https://manpages.debian.org/unstable/apt/apt.conf.5.en.html).
- [systemd-timesyncd — conservación de hora aproximadamente monotónica entre reinicios](https://www.freedesktop.org/software/systemd/man/250/systemd-timesyncd.html).
