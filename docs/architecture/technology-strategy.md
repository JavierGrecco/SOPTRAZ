# Estrategia tecnológica y núcleos por dominio

**Estado:** decisiones iniciales aceptadas; versiones exactas y dependencias se fijarán en manifiestos.

## Principio

Elegir tecnología por la fortaleza que aporta a una responsabilidad estable. No elegir un lenguaje distinto para cada módulo pequeño ni duplicar reglas de negocio entre lenguajes.

Un dominio tiene un núcleo canónico. Otros componentes lo consumen mediante contratos; no vuelven a implementar el mismo algoritmo.

## Matriz tecnológica

| Responsabilidad | Tecnología canónica | Motivo |
|---|---|---|
| Runtime, CLI, orquestación y servicios locales | Go | Binarios portables, concurrencia y APIs |
| Host desktop | Wails v3 | Go + Web UI en WebView nativo |
| UI | TypeScript + React + Vite | Ecosistema amplio, tipado y desacoplamiento |
| Analítica numérica, estadística y modelos | Python | Ecosistema científico/estadístico |
| Adaptación Windows | PowerShell y APIs nativas | Administración y reparación Windows |
| Adaptación macOS | APIs/utilidades nativas Apple | Respetar modelo operativo Apple |
| Adaptación Linux | APIs/utilidades nativas | Respetar diferencias entre distribuciones |
| Almacenamiento portable | SQLite | Base local embebida |
| Almacenamiento multiusuario | PostgreSQL | Concurrencia e integridad |
| IA remota/local | Adaptadores Go detrás de AI Gateway | Enrutamiento y políticas |
| Contratos | JSON Schema y OpenAPI | Interoperabilidad sin acoplamiento interno |

## Propiedad de la lógica

- Go posee lógica operacional: estados, autorizaciones, orquestación, dispatch, timeout, idempotencia, auditoría y políticas.
- Python posee algoritmos numéricos/analíticos: series temporales, detección estadística, tendencias, pronósticos, modelos y scoring cuantitativo.
- TypeScript presenta datos e interacción; no replica algoritmos de diagnóstico o scoring.
- PowerShell/Bash/zsh son adaptadores delgados; no contienen reglas de negocio duplicadas.
- SQL persiste y consulta; no se convierte en una segunda implementación de reglas de dominio.

## Cuándo usar Python

Usarlo cuando exista un problema analítico real que se beneficie de bibliotecas científicas. El motor se implementará una sola vez como paquete versionado y podrá ejecutarse como worker local o servicio del Hub.

El contrato será independiente del transporte. Primera opción IPC: stdin/stdout estructurado o pipes supervisados; no abrir un puerto innecesario. En Hub puede alojarse en un worker administrado.

No se usará Python para sumar un contador o formatear bytes. La regla es evitar duplicar algoritmos analíticos autoritativos, no forzar cada operación aritmética a otro proceso.

## Empaquetado Python

Field no requerirá que el usuario instale Python. Cuando exista una capacidad analítica concreta, se evaluará empaquetado por SO/arquitectura (PyInstaller o Nuitka) y se medirán tamaño, arranque, licencias y compatibilidad. Hasta entonces Python no es dependencia obligatoria del binario Go.

## UI y Wails

Wails v3 es el contenedor desktop elegido para el prototipo. Tiene soporte desktop estable para Windows, macOS y Linux, usa el WebView del sistema y requiere Go 1.26+. Linux necesita GTK/WebKit compatibles; se ofrecerá modo web local para hosts fuera de la matriz soportada.

## Versionado y reproducibilidad

- Go: versión estable soportada fijada en go.mod/toolchain.
- Node: versión LTS fijada para build, nunca requisito de runtime.
- Frontend: lockfile pnpm.
- Python: pyproject.toml y lockfile reproducible cuando se incorpore.
- Herramientas OEM/externas: versiones y hashes declarados.
- CI: pruebas por SO y arquitectura.
- Dependencias actualizadas mediante PR con changelog y pruebas.

## No hacer

- No introducir microservicios sin necesidad demostrada.
- No usar varios lenguajes para la misma regla.
- No duplicar modelos estadísticos en Go, Python y TypeScript.
- No ejecutar scripts arbitrarios generados por IA.
- No empaquetar dependencias sin revisar licencia y procedencia.
- No usar SQLite compartido en red para acceso multiusuario.
