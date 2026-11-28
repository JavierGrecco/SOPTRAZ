# ADR-0020: Fuente de tiempo confiable y recuperación ante relojes incorrectos

- Estado: Aceptada como política de arquitectura; implementación pendiente.
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
- **Roughtime, RFC 10049 (Experimental, octubre de 2026):** usa claves de largo plazo fijadas, un nonce fresco y una respuesta firmada con un intervalo de incertidumbre. El modo multi-servidor permite detectar inconsistencias entre fuentes; su ecosistema continúa siendo experimental y no se asumirá disponible en todos los sistemas.
- **Khronos, RFC 9523 (Informational, febrero de 2024):** propone selección y filtrado distribuido de muestras de muchos servidores NTP para resistir ataques de desplazamiento temporal. Es una referencia de investigación/industria, no una dependencia inicial de SOPTRAZ.
- **NTP sin autenticación** puede servir a la sincronización ordinaria del sistema, pero no se tratará como atestación criptográfica propia de SOPTRAZ.

La política concreta de fuentes, independencia, cantidad mínima de respuestas coincidentes, dispersión geográfica, incertidumbre máxima y rotación de claves debe quedar definida y probada antes de calificar una muestra como `authenticated`. No se fijará un quorum numérico sin especificar esas condiciones.

### Bootstrap temporal: resolver la dependencia circular de NTS

NTS-KE autentica su servidor con TLS y certificados X.509. La validación normal de `notBefore` y `notAfter` necesita una hora aproximadamente correcta; por eso NTS por sí solo no resuelve de forma limpia el primer contacto cuando el reloj carece de referencia. El RFC 8915 reconoce expresamente este problema y propone mitigaciones, sin afirmar que exista una solución perfecta.

SOPTRAZ adoptará una secuencia escalonada:

1. **Primera opción: hora segura ya disponible.** Usar RTC funcional y estado de sincronización del sistema operativo. Si el sistema informa que una fuente confiable ya disciplinó el reloj, validar certificados NTS normalmente.
2. **Recuperación cuando no hay hora correcta: fuente de bootstrap con claves fijadas.** Evaluar Roughtime con un conjunto de claves públicas provisionadas por un canal independiente de la conexión consultada. Verificar nonce, firma, delegación/certificado del servidor, intervalo de incertidumbre y respuestas inconsistentes entre servidores. La hora obtenida se usa primero como evidencia aproximada para recuperar el reloj del sistema; no se convierte automáticamente en una autorización para aceptar metadata TUF.
3. **Sincronización sostenida: NTS.** Una vez que la hora del sistema permite validar el certificado TLS del servidor NTS, usar NTS para la disciplina continua del reloj. SOPTRAZ verifica que la respuesta obtenida sea consistente con la validez del certificado NTS y con la última evidencia temporal aceptada.
4. **Sin internet o sin fuentes autenticadas:** no inventar una hora ni aceptar certificados por una excepción genérica. Se permite un procedimiento administrativo explícito para corregir aproximadamente el reloj, pero el estado de assurance será `system` hasta que exista evidencia autenticada. Para operaciones que requieran assurance más alto, se mantiene el bloqueo específico de la validación.

RFC 10049 describe Roughtime precisamente para dispositivos sin una idea inicial de la hora y explica que sus claves públicas de largo plazo funcionan como raíces de confianza. Sin embargo, es una RFC Experimental y no define por sí sola el ecosistema de publicación, gobernanza y rotación de listas de servidores. Por eso SOPTRAZ lo considera una opción de recuperación piloto detrás de una interfaz, no un requisito obligatorio ni una dependencia de producción inicial. La lista de servidores y claves deberá venir fijada en una versión firmada de SOPTRAZ o actualizarse mediante un canal independiente autenticado; nunca se aceptará una clave entregada por el mismo servidor cuya hora se está verificando.

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

## Referencias

- [TUF Specification 1.0.37 — fixed update time y sección 7.1 Support for bogus clocks](https://theupdateframework.github.io/specification/latest/).
- [The Update Framework — foro oficial: “Some questions about TUF” (respuesta de un mantenedor, 7 de abril de 2020)](https://groups.google.com/g/theupdateframework/c/OO6ClFXyBIA).
- [Uptane Standard 2.1.0](https://uptane.org/docs/latest/standard/uptane-standard).
- [Uptane Deployment Best Practices 2.0.0 — sección 3.1 Secure source of time y sección 4.2 Metadata expiration](https://uptane.org/docs/2.0.0/deployment/best-practices).
- [RFC 8915 — Network Time Security for NTP (Standards Track)](https://www.rfc-editor.org/rfc/rfc8915.html).
- [RFC 8915, sección 8.5 — Initial Verification of Server Certificates](https://www.rfc-editor.org/rfc/rfc8915.html#section-8.5): problema de bootstrap TLS con reloj incorrecto y mitigaciones.
- [RFC 10049 — Roughtime (Experimental, octubre de 2026)](https://www.rfc-editor.org/rfc/rfc10049.html).
- [RFC 9523 — Khronos (Informational, febrero de 2024)](https://www.rfc-editor.org/rfc/rfc9523.html).
- [APT apt.conf(5) — Check-Date y Check-Valid-Until](https://manpages.debian.org/unstable/apt/apt.conf.5.en.html).
- [systemd-timesyncd — conservación de hora aproximadamente monotónica entre reinicios](https://www.freedesktop.org/software/systemd/man/250/systemd-timesyncd.html).
