# Perfil de ejecución de procesos externos

**Estado:** contrato declarativo v1 implementado para procesos locales de solo lectura. El runner multiplataforma todavía no existe y ningún perfil puede iniciar un proceso.

## Finalidad

El perfil define el conjunto fijo de argumentos y los límites de una acción. No define una ruta del ejecutable ni autoriza por sí mismo su ejecución. El artefacto se identifica por `artifactId` y `artifactVersion`; antes de habilitar un runner, el catálogo TUF debe autenticar el descriptor completo y el resolver debe verificar los bytes instalados para el SO/arquitectura actuales.

## Restricciones de v1

- Solo `risk=read_only`, sin privilegios, sin aprobación y con comportamiento idempotente.
- Solo `executionMode=direct_argv`; el perfil no puede elegir shell, intérprete, comando ni ruta.
- `fixedArguments` son argumentos estáticos e inmutables. No existen plantillas, expansión, interpolación ni argumentos libres desde UI/IA.
- `networkAccess=none`; esta versión no autoriza conexiones de red originadas por el proceso.
- No se hereda ni se declara entorno arbitrario, directorio de trabajo, stdin, PATH ni variables sensibles.
- Timeout máximo de 300 segundos; hasta 32 argumentos, 4 KiB por argumento y 16 KiB en total.
- stdout limitado a 8 MiB y stderr a 256 KiB; formatos permitidos: none, text, JSON y JSONL.
- Los códigos de salida aceptados son explícitos, únicos y deben incluir cero.

Los metacaracteres de shell dentro de un argumento son datos literales. Solo un futuro runner que invoque directamente el ejecutable autenticado con un vector argv separado podrá consumir estos perfiles; nunca debe construir una cadena de comandos ni pasarla a un shell.

## Validación

El contrato está en `internal/contracts/schemas/process-profile.schema.json`, el modelo semántico en `internal/modules/supervisor/profile.go` y el fixture ilustrativo en `examples/contracts/process-profile.example.json`.

Validación local:

```sh
soptraz validate process-profile examples/contracts/process-profile.example.json
```

El fixture osquery es un ejemplo de estructura y consulta fija; no incluye un binario, no descarga herramientas y no acredita que una release externa esté autorizada.

## Puertas pendientes

1. Cerrar M1.4: bootstrap productivo de TUF, persistencia anti-rollback/offline y política de confianza.
2. Implementar un runner Windows/macOS/Linux que reciba solo un target TUF autenticado, verifique otra vez el archivo instalado inmediatamente antes de crear el proceso y ejecute sin shell.
3. Implementar terminación del árbol de procesos, cancelación, entorno mínimo, captura de stdout/stderr con cuotas y parsing estructurado.
4. Conectar evidencia/auditoría con IDs de acción e intervención y probar sustitución de binarios, hijos, timeout, salida excesiva y cierre abrupto.
5. Aceptar la primera POC osquery en laboratorio, opt-in y de solo lectura.

Hasta completar esas puertas, validar un perfil significa únicamente que su declaración cumple el contrato; no significa que se pueda ejecutar.
