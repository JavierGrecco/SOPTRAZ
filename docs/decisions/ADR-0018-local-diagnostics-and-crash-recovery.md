# ADR-0018: Informes locales de diagnóstico y recuperación de ejecución

- Estado: Aceptada; primera implementación en PR #31.
- Fecha: 2026-11-28.
- Relacionada: ADR-0001, ADR-0006, ADR-0012 y ADR-0017.
- Alcance: diagnóstico de fallos de proceso y ciclo de vida del runtime local de SOPTRAZ.

## Contexto

Los errores rutinarios no deben inundar al técnico con archivos. Sin embargo, un panic, un error fatal, un cierre forzado o una caída del equipo necesitan dejar evidencia útil para soporte. Los mensajes de consola desaparecen cuando se cierra la ventana y un proceso terminado por fuerza puede no ejecutar sus deferred functions.

SOPTRAZ debe distinguir los errores esperados de entrada/configuración de los defectos o fallos operativos que requieren diagnóstico.

## Decisión

1. Mantener un paquete transversal `internal/diagnostics` independiente del dominio de auditoría.
2. Crear informes de texto locales por ejecución en la carpeta de caché del usuario, dentro de `SOPTRAZ/diagnostics`.
3. Registrar solo severidades `ERROR`, `CRITICAL`, `FATAL` y `PANIC`; ignorar `INFO` y `WARN` rutinarios.
4. Capturar panics recuperados en el límite de proceso y configurar `runtime/debug.SetCrashOutput` para duplicar el diagnóstico del runtime Go a un archivo.
5. Cada informe comienza con una marca de ejecución; un cierre ordenado escribe `CIERRE NORMAL`.
6. Si falta esa marca, el siguiente arranque identifica una ejecución inconclusa y añade `runtime.unexpected_shutdown`, dejando la causa como indeterminada.
7. Usar locks de sistema operativo en informes individuales y un lock breve de arranque para distinguir procesos todavía activos y evitar carreras durante la recuperación.
8. No recopilar automáticamente argumentos, variables de entorno, archivos ni payloads. Aplicar redacción de credenciales en los campos estructurados.
9. Los informes quedan locales, con tamaño máximo de 4 MiB por archivo y retención de los últimos 30. Las ejecuciones sin incidencias eliminan el informe temporal al cerrar.
10. Si el almacenamiento de diagnósticos no está disponible, SOPTRAZ informa el problema por stderr y continúa; logging no puede ser requisito para iniciar el producto.

## Alternativas consideradas

- Solo escribir a stderr: insuficiente para un proceso cuyo terminal puede cerrarse o no estar disponible.
- Guardar todos los logs, incluidos INFO/WARN: produce ruido, consume espacio y dificulta la depuración.
- Suponer que se puede registrar SIGKILL o un corte de energía en tiempo real: técnicamente imposible en el proceso que ya no puede ejecutar instrucciones.
- Enviar automáticamente logs a un servidor: contradice la prioridad local y amplía innecesariamente la exposición de información.

## Consecuencias

- El técnico dispone de un TXT con contexto de ejecución, error importante y stack cuando el runtime lo puede proporcionar.
- Una terminación forzada se informa en el siguiente inicio, pero solo como terminación inesperada si no existe evidencia del motivo.
- El equipo de soporte puede pedir al usuario que revise y comparta voluntariamente el informe después de verificar que no contiene datos sensibles.
- Los módulos necesitan clasificar explícitamente qué errores son realmente importantes; no se registran automáticamente todos los errores de validación o de entrada.
- Fallos del propio disco pueden impedir crear o sincronizar el informe; el producto continúa y comunica esa limitación.
- La primera integración cubre el proceso CLI. Field/Hub y procesos auxiliares deberán conectarse al mismo servicio de diagnósticos mediante el ciclo de vida compartido.

## Evidencia y límites de la primera implementación

PR #31 incorpora el logger local, salida fatal del runtime Go, recuperación de ejecuciones sin cierre normal, redacción básica, tamaño y retención, e integración en `cmd/soptraz`. Las pruebas cubren cierre limpio, INFO/WARN excluidos, credenciales redactadas, panic con stack, ejecución activa simultánea y terminación abrupta simulada mediante un proceso hijo que omite `Close()`.
