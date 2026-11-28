# Plan director: estrategia de entrega e integraciones

**Fecha de corte:** 2026-11-28  
**Propósito:** convertir el roadmap técnico de SOPTRAZ en una secuencia de entregas comprobables, evitando tanto el endurecimiento indefinido de la infraestructura como la incorporación prematura de herramientas externas.

## 1. Decisión ejecutiva

SOPTRAZ ya tiene una fundación útil. La prioridad no es añadir más abstracciones generales ni empezar una colección de integraciones. La prioridad es:

1. cerrar con revisión manual el bloque de resiliencia y diagnósticos actualmente propuesto en PR #31;
2. hacer reproducible el entorno local de desarrollo y prueba;
3. terminar las puertas de seguridad M1.4 (confianza TUF) y M1.5 (supervisor de procesos);
4. construir una primera vertical funcional de diagnóstico de solo lectura, empezando por una integración acotada de osquery;
5. recién entonces ampliar la interfaz Field y habilitar acciones reversibles, una por una.

El mapa de integraciones se prepara en paralelo, pero ninguna integración obtiene permiso para ejecutar herramientas o modificar el equipo por el solo hecho de estar documentada.

**No se fija un porcentaje global ni una fecha de finalización artificial.** La aceptación se determina por capacidades verificables y pruebas sobre el commit exacto.

## 2. Foto real del repositorio al corte

- Rama principal: `main`, commit `ca2052c80ecfc942ea43ad122f0e2196ba7fb756`.
- Ramas visibles: `main` y `fix/tuf-crash-stage-recovery`.
- Trabajo abierto de código: PR #31, en borrador, con 117 commits, 40 archivos y aproximadamente 3.858 líneas añadidas.
- CI #244 para el SHA `b64d1123e47131705c28674d5c577a733d0a8c79` terminó correctamente con 6/6 trabajos: checks Go en Windows/macOS/Ubuntu, Race Detector, fuzzing y pruebas de higiene de ramas. [Ver ejecución #244](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46111218816).
- PR #31 **todavía no está integrado en main**. CI verde es condición necesaria, no aprobación automática de un cambio grande de seguridad. [Revisar PR #31](https://github.com/JavierGrecco/SOPTRAZ/pull/31).

### 2.1 Qué existe en main

- CLI Go de lectura/validación, contratos JSON versionados y modelo de intervención.
- Auditoría append-only con hash chain y persistencia SQLite.
- Primitivas de checkpoint Ed25519 y ECDSA P-256; no equivalen todavía a una custodia/enrolamiento de claves de producción.
- Descriptor de artefactos y resolver local confinado, que verifica plataforma, ruta, tamaño y hash. No descarga ni ejecuta binarios.
- Cliente TUF oficial para resolver targets/delegaciones online, persistencia de la cadena root y generaciones de metadata en SQLite, con staging aislado.
- Perfil inicial de dispositivo Windows de solo lectura, con evidencia y estados `unknown` explícitos.

### 2.2 Qué no debe darse por terminado

- M1.4: bootstrap de root de producción, política operativa de threshold/rotación/revocación, validación offline segura y revisión de SBOM/dependencias. SQLite local protege consistencia, pero restaurar un backup completo antiguo puede retroceder el high-water local; el límite y cualquier ancla externa deben quedar explícitos.
- M1.5: supervisor de procesos, ejecución sin shell, argumentos tipados, aislamiento, cancelación, límites y auditoría.
- La integración del logger en PR #31 no constituye telemetría automática global: se inicializa en la CLI y los módulos reciben el registrador mediante inyección explícita.
- No hay todavía una aplicación de escritorio Field funcional ni una UI Wails/React en el árbol principal.
- No hay todavía un producto de diagnóstico/reparación de extremo a extremo ni conectores Hub desplegados.

## 3. Qué hacer con la base

### Mantener y cerrar

Conservar las decisiones que ya dan valor: monolito modular, contratos versionados, un núcleo canónico por dominio, SQLite local, TUF oficial, auditoría verificable, operación offline-first y privilegio mínimo.

Terminar únicamente los requisitos que desbloquean operaciones reales seguras: integridad de catálogo, recuperación, proceso supervisor, autorización, evidencia y rollback.

### Dejar de hacer por ahora

- No crear nuevas capas genéricas de abstracción sin un caso de uso real.
- No empezar microservicios.
- No desarrollar un motor estadístico Python antes de tener un caso cuantitativo concreto y datos de entrada definidos.
- No crear una UI completa antes de estabilizar los contratos y un flujo de diagnóstico vertical.
- No incorporar más herramientas externas mientras M1.4/M1.5 no permitan consumirlas con garantías.

## 4. Roadmap por puertas de aceptación

Las fases se cierran por evidencia, no por cantidad de commits.

### Puerta 0 — Revisar y estabilizar PR #31

**Objetivo:** decidir con evidencia si el bloque de recuperación TUF y diagnósticos puede integrarse.

Trabajo obligatorio antes de sacar el PR del borrador:

- revisar el diff completo y el ciclo de vida del lock TUF, incluido el efecto de mantenerlo durante la vida del cliente;
- revisar la validación de rutas y permisos Windows en un equipo administrado, especialmente DACLs/grupos de dominio no cubiertos por los SIDs incorporados;
- probar el trust store SQLite en un volumen real con poco espacio/ENOSPC y revisar la durabilidad/sidecars por filesystem; la fault injection VFS no reemplaza esta prueba física;
- revisar sanitización, retención y contenido de informes técnicos antes de compartirlos;
- comprobar que el composition root inyecta el logger en los resolver/TUF que realmente se utilizan, y documentar con claridad los módulos que aún no lo reciben;
- probar los casos de cierre ordenado, panic recuperable y detección del cierre no ordenado al siguiente arranque.

**Aceptación:** revisión manual resuelta, tests pertinentes ampliados, CI verde sobre el SHA final exacto, documentación alineada y PR revisado. No hacer merge únicamente porque #244 está verde.

### Puerta 1 — Entorno local reproducible

**Objetivo:** que el desarrollador pueda clonar, verificar, probar y diagnosticar sin reconstruir manualmente el entorno cada vez.

Primera etapa: entorno Go del core, porque es la única dependencia de desarrollo necesaria hoy para la CLI y los módulos actuales.

- fijar Go desde `go.mod` (actualmente 1.27.4) y respetar `GOTOOLCHAIN=local`;
- documentar instalación y comprobación por Windows, macOS y Linux;
- entregar comandos consistentes para `gofmt`, `go vet ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go mod tidy -diff` y `go mod verify`;
- incorporar un comando/script de diagnóstico del entorno que muestre versiones, SO/arquitectura, herramientas faltantes y comandos ejecutados, sin cambiar el sistema;
- incluir una guía de solución para errores frecuentes y una salida de diagnóstico copiable;
- probar el flujo en una máquina local Apple Silicon/macOS y en Windows real, no depender solo de runners CI.

No instalar Node, pnpm, Wails ni Python como requisitos globales antes de que sus respectivas etapas los necesiten. Cuando comience Field, incorporar Node/pnpm con versiones y lockfile fijados; cuando exista analítica cuantitativa concreta, empaquetar el runtime Python para que el usuario final no tenga que instalarlo.

**Aceptación:** clon limpio → chequeo de entorno → build/tests locales → resultado reproducible, sin pasos secretos ni dependencias no documentadas.

### Puerta 2 — Cerrar M1.4: confianza TUF

**Objetivo:** que SOPTRAZ pueda saber qué herramienta está autorizada por un repositorio confiable, incluso después de reinicios y fallos.

- definir el bootstrap del root por un canal autenticado e independiente; no aceptar como confiable una root descargada sin pin;
- definir threshold, custodios, rotación, revocación y respuesta ante compromiso. No guardar claves privadas de producción en el repositorio;
- preservar la persistencia transaccional ya implementada, y documentar que el store local no resuelve por sí solo la reversión de un backup completo del mismo disco;
- construir un verificador offline explícito sobre metadata previamente verificada, vigente y monotónica, con delegaciones y expiraciones; sin TOFU ni `UnsafeLocalMode` como bypass;
- cubrir con fixtures deterministas rotación consecutiva, thresholds, firmas inválidas, expiración, rollback/freeze, consistent snapshots, delegaciones, caché corrupta, reinicio, corte de red y recuperación;
- revisar dependencias transitivas, avisos, licencias y SBOM.

**Aceptación:** ninguna herramienta se considera confiable solo por tener un hash local; el publicador y la metadata deben estar autenticados. Fallos o falta de evidencia producen rechazo seguro.

### Puerta 3 — Cerrar M1.5: supervisor de procesos

**Objetivo:** aislar herramientas de terceros para que un fallo, timeout o salida inesperada no comprometa SOPTRAZ ni otras intervenciones.

- resolver el ejecutable exclusivamente desde un target TUF autenticado y un descriptor validado;
- ejecutar sin shell; argumentos tipados y allowlist por acción;
- construir un entorno mínimo y no heredar variables sensibles;
- definir timeout, cancelación real y terminación del árbol de procesos por SO;
- limitar bytes/filas/tiempo de stdout y stderr; validar JSON/JSONL antes de consumirlo;
- registrar versión, hash, procedencia, acción, parámetros sanitizados, duración, exit code, resultado, evidencia e ID de intervención;
- fallar cerrado ante sustitución del binario, firma/hash incorrecto, salida malformada, proceso hijo fuera de control o incompatibilidad.

**Aceptación:** tests de unidad, integración y seguridad en Windows, macOS y Ubuntu; Race Detector y fuzzing donde corresponda. No habilitar una ejecución genérica desde la UI o desde IA.

### Puerta 4 — Primera integración real: osquery read-only

**Objetivo:** ofrecer inventario y consultas de diagnóstico útiles sin crear un agente residente ni una interfaz de SQL libre.

Secuencia:

1. ficha de integración: upstream, release, licencia, artefactos por SO/arquitectura, firma/hash, avisos, capacidades y límites;
2. paquete fijado y autenticado por TUF;
3. adaptador SOPTRAZ con consultas predefinidas, versionadas y revisadas;
4. extensiones deshabilitadas; sin servicio residente en la primera etapa;
5. sin SQL libre desde UI/IA; sin privilegios elevados para consultas básicas;
6. límites de ejecución y resultados; salida estructurada validada;
7. ausencia/incompatibilidad de la herramienta = capacidad no disponible, nunca descarga silenciosa ni fallback a un ejecutable encontrado en PATH.

**Aceptación:** una POC opt-in de inventario/consultas de solo lectura en máquinas de laboratorio, con evidencias auditables y sin mutaciones.

### Puerta 5 — Vertical de diagnóstico de extremo a extremo

Consolidar una primera experiencia útil antes de ampliar el catálogo:

- perfil del equipo con confianza, evidencia y fuentes parciales;
- sistema operativo/build/arquitectura, CPU/RAM, almacenamiento/espacio, red/DNS y software básico;
- SMART cuando exista una fuente fiable y compatible;
- reporte local con hallazgos, severidad, evidencia, límites y recomendaciones;
- un ID de intervención para correlacionar solicitud, recolección, hallazgos, aprobación y reporte;
- ejecución offline de las capacidades nativas ya disponibles; los conectores externos permanecen opcionales.

**Aceptación:** un técnico inicia una revisión, obtiene un reporte reproducible, entiende de dónde viene cada dato y puede exportar el resultado sin enviar información a la nube.

### Puerta 6 — Field: interfaz de escritorio

Solo después de estabilizar el flujo de diagnóstico:

- Wails v3 + React/TypeScript como presentación; Go conserva la lógica y autorización;
- UI adaptativa al equipo detectado: tablet/touch, convertible, notebook, desktop, virtual/remoto;
- vista inicial de estado del equipo, diagnóstico, evidencias, herramientas disponibles e informes;
- estados de capacidad claros: disponible, parcial, no disponible, requiere privilegio y requiere conexión;
- interfaz táctil con áreas de interacción amplias y navegación simplificada; sin lógica de sistema en la UI;
- empaquetado y pruebas reales de instalación/actualización/desinstalación por SO/arquitectura.

No intentar construir a la vez toda la suite de mantenimiento, reparación y Hub.

### Puerta 7 — Primeras acciones Windows reversibles

Después de M1.4 y M1.5, comenzar con acciones individuales de bajo riesgo. Cada acción debe declarar sistema/build/arquitectura admitidos, precondiciones, riesgo, privilegios, estado previo, idempotencia, timeout, verificación posterior y tipo de rollback.

Los perfiles Mínimo, Equilibrado, Gaming, Batería, Privacidad y Personalizado serán selecciones visibles de acciones compatibles, no scripts opacos. No deshabilitar por defecto Defender, Firewall, UAC, BitLocker, Secure Boot, VBS, Windows Update ni mitigaciones de CPU. No prometer mejoras de FPS/latencia/RAM sin medición reproducible.

### Puerta 8 — IA local/remota y Hub

- AI Gateway solo cuando existan diagnósticos y contratos de salida fiables; la IA explica y prioriza, no adquiere permisos ni ejecuta comandos libres.
- Ollama como adaptador local opcional; el core no depende de que esté instalado.
- Hub después de tener exportación/importación Field → Hub idempotente, custodia de claves y modelo de roles/organizaciones/activos.
- GLPI y Fleet se conectan cuando exista una necesidad organizacional real; no duplicar sus funciones de ITSM o gestión de endpoints.

## 5. Mapa de integraciones

| Prioridad | Proyecto/familia | Tratamiento propuesto | Momento/condición |
|---|---|---|---|
| 1 | [osquery](https://github.com/osquery/osquery) | Adoptar como adaptador opcional read-only; consultas aprobadas, extensiones apagadas, sin daemon inicial | Después de M1.4 y M1.5 |
| 2 | [smartmontools](https://www.smartmontools.org/) | Adaptador de salud/SMART, capacidades diferenciadas por SO/dispositivo | Durante la vertical de diagnóstico |
| 3 | [Microsoft Sysinternals](https://learn.microsoft.com/sysinternals/) | Herramientas opcionales de diagnóstico Windows; respetar firma y derechos de distribución | Según cada caso, tras el supervisor |
| 4 | [WinUtil](https://github.com/ChrisTitusTech/winutil) y [Sophia Script](https://github.com/farag2/Sophia-Script-for-Windows) | Fuentes para estudiar acciones y experiencia; evaluar y portar/adaptar solo acciones concretas con licencia, precondiciones y rollback verificados | M1.6; no ejecutar scripts completos ni importar presets a ciegas |
| 5 | [WinGet](https://github.com/microsoft/winget-cli) | Conector para inventario/gestión de paquetes; configuración declarativa revisada | Tras supervisor y catálogo |
| 6 | [Cosign](https://github.com/sigstore/cosign) | Verificación complementaria de identidad/attestations de releases; no sustituye TUF | Al diseñar la distribución de SOPTRAZ |
| 7 | [Ollama](https://ollama.com/) | Adaptador opcional de inferencia local detrás de AI Gateway | Fase IA |
| 8 | [GLPI](https://github.com/glpi-project/glpi) y [Fleet](https://github.com/fleetdm/fleet) | Conectores Hub para tickets/activos y endpoint management; evitar duplicar plataformas maduras | Hub |
| 9 | OEM (Dell, HP, Lenovo, Intel, AMD, NVIDIA) | Adaptadores por fabricante, modelo, hardware ID y build; inventario antes que instalación | Driver Intelligence |
| 10 | [Ventoy](https://github.com/ventoy/Ventoy), [SystemRescue](https://www.system-rescue.org/) y [Rescuezilla](https://rescuezilla.com/) | Distribuir/invocar medios oficiales de rescate cuando licencia y procedencia lo permitan; no desarrollar otro boot manager o clonador | Fase Recovery |
| Exploratoria | Action1, PDQ, MeshCentral y otras plataformas | Conectar por API/CLI y opt-in; nunca hacerlas dependencias del núcleo Field | Solo tras caso de uso validado |
| Referencia, no dependencia | Atlas-OS, MiniOS/DELTA y Sysora Office Kit | Estudiar UX, catálogo y procedimientos; revisar licencia, permisos, API y redistribución antes de cualquier reutilización | Investigación; no copiar ni redistribuir sin autorización verificada |

### 5.1 Regla específica para WinUtil y Sophia

No integrarlos como un bloque. Cada tweak o función candidata se convierte en una ficha SOPTRAZ independiente: origen y licencia, versión de Windows admitida, condición del hardware, estado previo, efecto esperado, riesgos, privilegios, verificación y restauración. Una función sin rollback verificable no entra en perfiles automáticos. Las opciones que debiliten controles de seguridad quedan fuera de los perfiles recomendados.

## 6. Cómo se integrará cada proyecto

Toda integración nueva debe pasar por esta ficha antes del código:

1. **Necesidad:** qué problema real resuelve y por qué no alcanza una capacidad nativa existente.
2. **Upstream:** URL oficial, mantenedor, actividad, release y política de soporte.
3. **Derechos:** licencia del código, binarios, assets, documentación y dependencias; obligaciones de redistribución.
4. **Artefacto:** versión, SO/arquitectura, tamaño, SHA-256, firma/procedencia, avisos y SBOM.
5. **Contrato SOPTRAZ:** entradas/salidas tipadas, versión, capabilities y errores conocidos.
6. **Seguridad:** permisos mínimos, superficie de red, shell, scripts, extensiones, rutas y secretos.
7. **Operación:** timeout, cancelación, límites, aislamiento, idempotencia, no disponibilidad y concurrencia.
8. **Evidencia:** datos recogidos, fuente, timestamp, calidad, sanitización y auditoría.
9. **Recuperación:** qué cambia, estado previo, rollback exacto/parcial/no disponible y límites.
10. **Prueba:** fixture determinista, laboratorio, matriz SO/arquitectura y aceptación del técnico.
11. **Mantenimiento:** responsable, actualización, compatibilidad, retirada y sustitución.

La integración es un adaptador sustituible. SOPTRAZ conserva el ciclo de intervención, las políticas, la interfaz, la evidencia y la auditoría; el proyecto externo conserva su identidad, licencia y ciclo de releases.

## 7. Entorno y estrategia de pruebas

### Herramientas necesarias ahora

- Go 1.27.4 fijado por `go.mod`.
- Git y los comandos de compilación/prueba indicados en CONTRIBUTING.
- Node/pnpm solo al crear el frontend; Python solo al incorporar el motor analítico.
- Sin Docker obligatorio para la CLI/core actual.

### Matriz de validación

| Nivel | Propósito |
|---|---|
| CI Windows/macOS/Ubuntu | Compilación, formato, vet y tests de plataforma |
| Race Detector | Concurrencia y estado compartido |
| Fuzzing | Parsers, contratos y entradas hostiles |
| Fault injection | Fallos SQLite, red, locks, procesos y filesystem |
| Laboratorio físico/VM | Hardware real, Windows administrado, rollback y reinicios |
| Prueba local de desarrollador | Reproducir comandos y depurar antes de abrir PR |

CI verde no reemplaza pruebas sobre hardware físico, archivosystems reales, políticas de dominio o escenarios de recuperación.

## 8. Política de ramas y cambios

- `main` es la única rama de larga vida.
- Una rama corta por objetivo coherente; no mezclar documentación, refactors y funcionalidad sin necesidad.
- No apilar cambios dependientes salvo justificación explícita.
- No cancelar pruebas solo para introducir otro cambio; dejar que cada SHA complete su suite.
- Cada PR declara alcance, riesgo, evidencia, limitaciones y rollback.
- Integrar por squash cuando corresponda y borrar únicamente ramas cuya integración esté demostrada.
- Los cambios de seguridad grandes requieren revisión del diff y pruebas manuales adicionales, aunque el CI esté verde.

## 9. Próximas tres entregas recomendadas

1. **Cerrar revisión del PR #31:** resolver el checklist manual y decidir merge/retrabajo con base en evidencia. Mantener la rama sin cambios adicionales mientras se revisa.
2. **Formalizar entorno local:** guía y verificador de entorno Go para clonado limpio, tests locales y diagnóstico, empezando por macOS y verificando también Windows.
3. **Terminar M1.4 y después M1.5:** cerrar raíz confiable/offline y supervisor antes de habilitar osquery.

En paralelo a esas tres entregas se completan fichas de integración, pero no se abre un segundo frente de ejecución externa.

## 10. Criterio de éxito de esta estrategia

SOPTRAZ deja de ser solamente una base arquitectónica cuando un técnico puede iniciar una intervención Field, obtener un diagnóstico de solo lectura reproducible, entender la evidencia y sus límites, ejecutar una herramienta externa autenticada y aislada cuando está disponible, y generar un informe local auditable que sigue siendo útil si la conexión a Internet no existe.

Ese flujo mínimo es el punto de inflexión. El resto —optimización, drivers, recuperación, IA, Hub y conectores— crecerá como módulos sobre esa experiencia comprobada, no como funcionalidades independientes sin una columna vertebral operativa.
