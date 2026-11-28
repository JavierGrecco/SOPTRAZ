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

Cada ejecución que produce un incidente deja un archivo `soptraz-run-*.txt`. Se aplican permisos restrictivos donde el sistema operativo los admite. Se conservan los últimos 30 informes; cada archivo está limitado a 4 MiB y cada registro a 64 KiB. Las ejecuciones normales sin eventos importantes eliminan su informe temporal al cerrar.

## Qué se registra

- `ERROR`, `CRITICAL`, `FATAL` y `PANIC`.
- Componente, operación, código, descripción y causa conocida.
- Versión de SOPTRAZ, sistema operativo, arquitectura, runtime Go, PID y hora UTC.
- Stack trace para panics recuperados.
- Mensaje del runtime cuando se produce un panic no manejado o un fallo fatal de Go.
- Detección posterior de una ejecución sin marca de cierre normal.

`INFO` y `WARN` rutinarios no se escriben en el informe. Los módulos pueden enviar eventos importantes con `diagnostics.Logger.Record`; los errores esperados por validaciones del usuario no deben clasificarse como fallos críticos.

## Recuperación de terminaciones inesperadas

Al comenzar, SOPTRAZ inspecciona informes de ejecuciones anteriores. Un lock de sistema operativo por archivo distingue una ejecución todavía activa de una que abandonó el proceso sin ejecutar `Close()`. Un lock adicional serializa la inspección y creación de informes entre varios procesos.

Si falta la marca `CIERRE NORMAL`, SOPTRAZ añade al informe previo un evento `runtime.unexpected_shutdown` y lo vincula desde el informe de recuperación. La causa se deja expresamente como indeterminada: puede haber sido un `kill`, un panic/fatal, un corte de energía o una terminación del sistema.

El lock del informe se libera automáticamente al terminar el proceso. Esta misma propiedad se usa en el catálogo TUF para permitir que el próximo inicio limpie stages huérfanos solo después de adquirir el lock exclusivo del repositorio.

## Qué puede y qué no puede capturarse

- Un panic recuperable en el límite principal de la CLI se registra con su stack y termina con código de salida no cero.
- `runtime/debug.SetCrashOutput` dirige el diagnóstico de panics no manejados y fallos fatales del runtime Go a un archivo adicional, además de stderr.
- Un `kill -9`, un corte de energía o una detención del sistema no permiten ejecutar código de cierre. En esos casos se detecta la falta de cierre normal en el próximo inicio; no se inventa un stack ni una causa que el proceso no pudo guardar.
- Un fallo de disco, permisos o espacio insuficiente puede impedir la escritura del informe. La aplicación continúa su operación y avisa por stderr; el registro no debe convertirse en requisito para poder arrancar.

## Privacidad y seguridad

- No se vuelcan variables de entorno, argumentos completos, payloads JSON ni contenido de archivos.
- Los campos de evento se limitan, pasan a una sola línea y se filtran patrones comunes de contraseñas, tokens, credenciales en URL y claves de query.
- Las pilas y mensajes nativos de Go pueden incluir nombres de archivos, rutas y texto de panic; el técnico debe revisar el informe antes de compartirlo.
- No se adjunta ni transmite automáticamente ningún informe.
- El directorio y los archivos usan permisos restrictivos donde la plataforma lo permite. En Windows la protección efectiva depende también de la ACL del perfil de usuario.

## Límites y evolución

Esta primera capa cubre el ciclo de vida de la CLI y proporciona una API para registrar eventos críticos desde módulos. Todavía no es un servicio central de observabilidad para procesos auxiliares, UI Field/Hub ni agentes remotos. La exportación redactada, correlación por intervención, retención configurable y envío voluntario por el técnico requieren contratos y controles adicionales.
