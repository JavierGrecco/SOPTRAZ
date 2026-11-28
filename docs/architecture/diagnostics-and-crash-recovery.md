# Diagnósticos locales y recuperación tras fallos

- Estado: primera implementación en `internal/diagnostics`; se integra en el arranque de la CLI mediante PR #31.
- Alcance: informes técnicos locales para fallos importantes, panics y terminaciones inesperadas. No sustituye la auditoría de intervenciones ni registra actividad rutinaria.

## Objetivo

El técnico o el equipo de soporte debe disponer de un archivo de texto entendible cuando SOPTRAZ sufra un fallo importante. El informe tiene que señalar el componente, operación, código de error, descripción, causa disponible, versión y entorno de ejecución, además de la pila cuando corresponda.

Los informes permanecen en el equipo. SOPTRAZ no envía automáticamente trazas, nombres de usuario, argumentos, contenido de contratos ni datos a servicios externos.

## Dónde se guardan

La CLI utiliza la carpeta de caché local del usuario:

- Windows: carpeta local de datos del usuario.
- macOS: caché del usuario.
- Linux: `XDG_CACHE_HOME` o la ubicación de caché del usuario.

Subcarpeta: `SOPTRAZ/diagnostics`.

Cada ejecución crea un informe temporal `soptraz-run-*.txt`; si termina normalmente sin errores importantes, el archivo se elimina al cerrar. Los informes con incidentes quedan en esa carpeta. Se conservan hasta 30 informes, protegiendo el que acaba de recuperarse durante el inicio actual. Los eventos estructurados se limitan a 64 KiB y 4 MiB por archivo; un volcado nativo excepcionalmente grande del runtime se recorta al siguiente inicio, preservando el encabezado, los últimos 64 KiB de la traza y su marca de cierre o recuperación. El contenido intermedio se omite para respetar el límite. Se aplican permisos restrictivos donde el sistema operativo los admite. Para encontrar la carpeta exacta en cualquier equipo, ejecutar `soptraz diagnostics-path`.

## Qué se registra

- `ERROR`, `CRITICAL`, `FATAL` y `PANIC`.
- El comando `soptraz diagnostics-path` para que el técnico localice los archivos sin conocer rutas específicas del sistema operativo.
- Componente, operación, código, descripción y causa conocida.
- Versión de SOPTRAZ, sistema operativo, arquitectura, runtime Go, PID y hora UTC.
- Stack trace para panics recuperados.
- Mensaje del runtime cuando se produce un panic no manejado o un fallo fatal de Go.
- Detección posterior de una ejecución sin marca de cierre normal.

`INFO` y `WARN` rutinarios no se escriben en el informe. La interfaz `diagnostics.Logger.Record` se inyecta explícitamente: el resolver de artefactos puede registrar discrepancias de integridad, enlaces/reparse points no permitidos, sustitución de rutas y errores de lectura durante la verificación. Los errores esperables de descriptor, plataforma o ausencia de un artefacto no se clasifican como incidentes. La integración requiere que el composition root utilice `NewResolverWithDiagnostics`; no existe un registrador global automático.

## Recuperación de terminaciones inesperadas

Al comenzar, SOPTRAZ inspecciona informes de ejecuciones anteriores. Cada informe tiene un archivo lateral `.lock` independiente del TXT: el bloqueo nunca se aplica al propio archivo que recibe escrituras. Esto es importante en Windows, donde un lock por rango de bytes sobre el TXT impediría escribir desde otro descriptor. El lock lateral distingue una ejecución todavía activa de una que abandonó el proceso sin ejecutar `Close()`. Un lock adicional serializa la inspección y creación de informes entre varios procesos.

Si no existe `CIERRE NORMAL` ni una marca previa `RECUPERACIÓN REGISTRADA`, SOPTRAZ añade al informe anterior un evento `runtime.unexpected_shutdown` y una marca de recuperación. Así el mismo incidente se notifica una sola vez y no se presenta en cada inicio como un fallo nuevo. La causa se deja expresamente como indeterminada: puede haber sido un `kill`, un panic/fatal, un corte de energía o una terminación del sistema. El informe recuperado queda protegido frente a la limpieza de retención durante ese arranque.

El lock del informe se libera automáticamente al terminar el proceso. Esta misma propiedad se usa en el catálogo TUF para permitir que el próximo inicio limpie stages huérfanos solo después de adquirir el lock exclusivo del repositorio.

## Qué puede y qué no puede capturarse

- Un panic recuperable en el límite principal de la CLI se registra con su stack y termina con código de salida no cero.
- `runtime/debug.SetCrashOutput` dirige el diagnóstico de panics no manejados y fallos fatales del runtime Go a un archivo adicional, además de stderr.
- Un `kill -9`, un corte de energía o una detención del sistema no permiten ejecutar código de cierre. La prueba de recuperación utiliza un proceso hijo que termina con `os.Exit` sin ejecutar `Close()` ni deferred cleanup. En esos casos se detecta la falta de cierre normal en el próximo inicio; no se inventa un stack ni una causa que el proceso no pudo guardar. La CLI muestra la ubicación de informes recuperados y `soptraz diagnostics-path` permite consultarla manualmente.
- Un fallo de disco, permisos o espacio insuficiente puede impedir la escritura del informe. La aplicación continúa su operación y avisa por stderr; el registro no debe convertirse en requisito para poder arrancar.

## Privacidad y seguridad

- No se vuelcan variables de entorno, argumentos completos, payloads JSON ni contenido de archivos.
- Los campos de evento se limitan, pasan a una sola línea y se filtran patrones comunes de contraseñas, tokens, credenciales en URL y claves de query.
- Las pilas y mensajes nativos de Go pueden incluir nombres de archivos, rutas y texto de panic; el técnico debe revisar el informe antes de compartirlo.
- No se adjunta ni transmite automáticamente ningún informe.
- El directorio y los archivos usan permisos restrictivos donde la plataforma lo permite. En Windows la protección efectiva depende también de la ACL del perfil de usuario.

## Límites y evolución

Esta primera capa cubre el ciclo de vida de la CLI y proporciona una API inyectable para eventos críticos desde módulos. El resolver de artefactos dispone de una integración explícita y selectiva; el cliente TUF y los demás módulos todavía no reportan incidentes automáticamente. Todavía no es un servicio central de observabilidad para procesos auxiliares, UI Field/Hub ni agentes remotos. La exportación redactada, correlación por intervención, retención configurable y envío voluntario por el técnico requieren contratos y controles adicionales.
