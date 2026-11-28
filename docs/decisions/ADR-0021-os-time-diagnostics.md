# ADR-0021: Diagnóstico de sincronización horaria del sistema operativo

- Estado: Aceptada como política de arquitectura; adaptadores pendientes.
- Fecha: 2026-11-28
- Relacionada: ADR-0020, ADR-0006, M1.4.
- Alcance: observación local, de solo lectura, de la hora de pared y de las señales de sincronización que exponga el sistema operativo. No establece una fuente de tiempo criptográficamente confiable.

## Contexto

ADR-0020 separa tres conceptos: hora de pared del sistema, evidencia temporal autenticada y memoria anti-rollback. El contrato internal/platform/trustedtime.Evidence representa un intervalo temporal con incertidumbre y procedencia. No debe usarse para envolver sin más el resultado de time.Now().

Una lectura local como time.Now() no lleva por sí sola un límite de error defendible. Convertirla en [t,t] afirmaría incertidumbre cero, algo que el sistema no puede demostrar. Del mismo modo, que un servicio NTP esté instalado, activo o habilitado no prueba que haya sincronizado recientemente, que su origen sea el esperado ni que el intercambio haya sido autenticado.

Necesitamos un diagnóstico útil y multiplataforma sin elevar privilegios automáticamente, cambiar la hora, modificar la configuración de red ni convertir señales locales en autorización para TUF.

## Decisión

### 1. Separar diagnóstico de evidencia confiable

El futuro tipo SystemTimeDiagnostics será independiente de trustedtime.Evidence. Como mínimo distinguirá:

- WallClockObservedAt: lectura UTC del reloj local tomada al capturar el diagnóstico; sirve para mostrar el contexto, no para validar expiración.
- ServiceState: active, inactive o unknown. Describe el servicio de sincronización observado.
- SynchronizationState: synchronized, not_synchronized o unknown. Describe lo que informa una interfaz local, no una verificación de una fuente externa.
- Mechanism: identificador estable del mecanismo observado (por ejemplo, w32time, systemd-kernel-clock o macos-timed).
- LastSyncAt y SourceHint: opcionales y solo cuando la interfaz nativa los exponga de forma estructurada y con semántica clara. Se redactarán antes de exportar o registrar.

Los estados unknown no se reemplazarán por suposiciones. Un servicio activo no implica que el reloj esté sincronizado. Un servicio habilitado no implica que haya completado una sincronización. Un diagnóstico ausente o denegado no será una falla fatal del programa.

### 2. Tope de confianza

Toda señal de esta capa tendrá clase system. No podrá elevarse a authenticated ni attested por el nombre del servicio, el estado synchronized, la pertenencia a un dominio, la presencia de un servidor configurado o un timestamp local.

El diagnóstico no actualiza LastAcceptedTrustedTime, no produce TrustedTimeEvidence, no valida expiración TUF, no desbloquea el catálogo offline y no devuelve VerifiedTarget. Una futura implementación deberá obtener por separado una evidencia temporal aceptada según ADR-0020.

### 3. Windows

La fuente operativa principal es Windows Time Service (W32Time). Microsoft documenta w32tm /query /source, /status y /configuration para consultar fuente, último éxito de sincronización y configuración. La misma documentación indica que ejecutar w32tm localmente requiere pertenecer al grupo Administradores.

Por lo tanto:

- No se elevará el proceso, no se solicitará UAC y no se ejecutará w32tm /resync, /config ni otra opción mutante.
- La primera implementación podrá consultar de forma read-only el estado del servicio W32Time a través de Service Control Manager si el permiso del usuario lo permite.
- La presencia/ejecución del servicio solo completará ServiceState. SynchronizationState quedará unknown hasta que se disponga de una interfaz estructurada compatible con el usuario actual.
- w32tm /query solo podrá evaluarse en una POC separada donde el proceso ya tenga los privilegios requeridos; no se diseñará la aplicación para elevarse solo con el fin de obtener una etiqueta de sincronización.
- Nunca se interpretará el origen mostrado por W32Time como autenticación criptográfica fuera de los contextos donde esa autenticación haya sido verificada explícitamente.

### 4. Linux

Se preferirá una consulta directa y read-only del estado de disciplina del reloj del kernel mediante adjtimex(2) con modes=0, si la prueba de concepto confirma compatibilidad en las arquitecturas soportadas. La consulta debe leer el estado existente; no debe establecer offset, frecuencia, leap status ni otros campos.

En equipos con systemd se podrá consultar opcionalmente la interfaz D-Bus org.freedesktop.timedate1 mediante propiedades de solo lectura:

- CanNTP: disponibilidad de un servicio de sincronización.
- NTP: si dicho servicio está habilitado.
- NTPSynchronized: si el kernel informa que el reloj está sincronizado.

NTPSynchronized refleja el criterio del kernel y no autentica al servidor, al operador ni al camino de red. NTP=true por sí solo nunca equivale a synchronized. No se invocarán los métodos mutantes SetTime, SetTimezone, SetLocalRTC ni SetNTP.

Las distribuciones sin systemd, con Chrony, ntpd, OpenNTPD u otro proveedor seguirán teniendo un resultado explícito unknown hasta que exista un adaptador específico probado. No se inferirá ausencia de sincronización solo por no encontrar systemd.

### 5. macOS

macOS mantiene el daemon nativo timed, que administra la sincronización con relojes de referencia y calcula incertidumbre internamente. La documentación pública revisada describe el daemon y sus archivos de estado, pero no ofrece en esa interfaz un contrato público y estructurado, de usuario normal, para leer la última sincronización y su incertidumbre.

Por tanto:

- Se podrá reportar presencia/estado del servicio solo mediante una interfaz pública y read-only cuya salida sea estable y se pruebe en las versiones soportadas.
- La sincronización efectiva será unknown mientras no exista una API pública y verificable que la exponga.
- No se leerá /var/db/timed/com.apple.timed.plist como si fuera una API: es estado cacheado interno del daemon.
- No se utilizará systemsetup para el sondeo normal: la documentación de Apple indica su ejecución bajo sudo, y sus opciones incluyen operaciones que pueden cambiar la hora o la configuración.
- No se inferirá synchronized por encontrar el proceso timed o su LaunchDaemon.

### 6. Seguridad y aislamiento

Todos los adaptadores serán de solo lectura y locales:

- No cambiarán el reloj, RTC, zona horaria, servidores NTP, peers, políticas ni servicios.
- No intentarán conectividad de red ni enviarán consultas propias a servidores temporales.
- No elevarán privilegios ni mostrarán UAC, sudo o diálogos de autorización.
- No ejecutarán shell ni aceptarán comandos, rutas, argumentos o variables de entorno aportados por el usuario.
- Si una consulta requiere un binario del sistema, su ruta y argumentos serán constantes y allowlisted; se usará exec.CommandContext sin shell, timeout corto, salida limitada y entorno mínimo. Se preferirán APIs nativas cuando sean estables y disponibles.
- Las respuestas crudas, hostnames, nombres de dominio, IPs y rutas no se escribirán en los logs de incidentes. Los errores se traducirán a códigos tipados y sanitizados.
- La indisponibilidad del servicio o la falta de permisos producirá unknown/unavailable, no detendrá el resto de SOPTRAZ.

## Pruebas y aceptación

1. Hora de pared incorrecta no se convierte en intervalo de incertidumbre cero ni en evidencia confiable.
2. Estado de servicio activo sin sincronización confirmada mantiene SynchronizationState=unknown.
3. En Linux, probar adjtimex read-only con estado sincronizado, STA_UNSYNC, syscall denegado/no soportado y diferentes arquitecturas.
4. En systemd, distinguir CanNTP, NTP y NTPSynchronized; ausencia de D-Bus o propiedad no disponible produce unknown.
5. En Windows sin privilegios administrativos, el sondeo no eleva ni lanza UAC y no marca la sincronización como verificada.
6. En Windows, probar servicio ausente, detenido, acceso denegado y dominios AD DS sin inferir assurance de la mera pertenencia al dominio.
7. En macOS, probar daemon ausente/activo, y no leer ni depender del plist cacheado privado; mantener sincronización unknown mientras no exista API pública.
8. No invocar ninguna operación mutante ni modificar registros, archivos de configuración o reloj del sistema.
9. Salida acotada, errores sanitizados, timeout, cancelación y context respetado.
10. Windows, macOS y Ubuntu; race detector y pruebas con fakes deterministas.

## Consecuencias

**Beneficios**
- La UI y los informes pueden explicar por qué el reloj parece estar sincronizado sin presentarlo como prueba criptográfica.
- El diagnóstico funciona con privilegios de usuario y tolera plataformas que no exponen estado suficiente.
- Se preserva la independencia entre diagnóstico, TrustedTimeEvidence y autorización del catálogo.

**Costos**
- La información disponible no será idéntica en los tres sistemas.
- Algunas plataformas reportarán unknown para sincronización efectiva.
- Las implementaciones deberán probar cada API/versión y no podrán asumir que el proveedor nativo usa NTS.

## Referencias

- [Microsoft Learn — Herramientas y configuración del servicio de hora de Windows](https://learn.microsoft.com/es-es/windows-server/networking/windows-time-service/windows-time-service-tools-and-settings?tabs=config): comandos de consulta W32Time y requisito de grupo Administradores para ejecución local.
- [Microsoft Learn — Windows Time for Traceability](https://learn.microsoft.com/en-us/windows-server/networking/windows-time-service/windows-time-for-traceability): eventos de estado, reloj y servicio W32Time.
- [systemd — org.freedesktop.timedate1 D-Bus API](https://github.com/systemd/systemd/blob/main/man/org.freedesktop.timedate1.xml): propiedades read-only CanNTP, NTP y NTPSynchronized.
- [systemd — timedatectl](https://www.freedesktop.org/software/systemd/man/latest/timedatectl.html): forma machine-readable show.
- [macOS — timed(8), manual page](https://man.freebsd.org/cgi/man.cgi?query=timed&sektion=8&manpath=macOS+26.7): daemon nativo, cálculo interno de incertidumbre y ubicación de estado cacheado.
- [Apple — systemsetup en Remote Desktop](https://support.apple.com/en-lk/guide/remote-desktop/apd95406b8d/mac): uso de sudo systemsetup y opciones de configuración horaria.
