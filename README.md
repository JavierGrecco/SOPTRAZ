# SOPTRAZ

**Intelligent IT Operations** — plataforma modular de diagnóstico, soporte, reparación, recuperación y gestión de operaciones IT.

SOPTRAZ se diseña como un producto propio que aprovecha herramientas y proyectos maduros. Su núcleo coordina módulos especializados, procedimientos verificables e integraciones; no intenta reimplementar herramientas que ya resuelven bien una necesidad concreta.

## Estado operativo y punto de entrada

**Prioridad actual: M1.4 — catálogo autenticado TUF.** Ya existen el cliente online oficial, la cadena root persistente, las generaciones SQLite y una lectura offline consistente. El replay criptográfico compartido está bajo revisión en [PR #50](https://github.com/JavierGrecco/SOPTRAZ/pull/50). **Todavía no existe un verificador offline de producción ni se autoriza descargar/ejecutar artefactos desde el catálogo.**

La guía [Trabajo activo y continuidad](docs/roadmap/active-work.md) es el punto único para saber qué falta, qué ramas están abiertas, qué pruebas ejecutar y cómo retirar cada tarea una vez aceptada. Incluye la secuencia de M1.4 y el plan para recuperar los cambios útiles de [PR #31](https://github.com/JavierGrecco/SOPTRAZ/pull/31) sin fusionar su rama divergente en bloque.

## Ediciones previstas

- **SOPTRAZ Field**: edición portátil, local y offline-first para técnicos.
- **SOPTRAZ Hub**: edición integrada para organizaciones, usuarios, equipos, activos, tareas, políticas y auditoría centralizada.

Ambas ediciones compartirán contratos, procedimientos, motor de ejecución, diagnóstico, seguridad y conectores. No serán dos productos con lógica duplicada.

## Principios no negociables

1. **Offline-first**: los diagnósticos y procedimientos locales fundamentales no dependen de Internet ni de una API de IA.
2. **Free-first AI**: la IA cloud usa únicamente proveedores/modelos gratuitos por defecto; no se permite gasto automático.
3. **Failover de IA**: cambia de proveedor ante cuota agotada, indisponibilidad o errores transitorios; Ollama/local es alternativa cuando esté instalado.
4. **Human-in-control**: la IA recomienda; el motor local valida; el técnico autoriza las operaciones según su riesgo.
5. **Un algoritmo, un núcleo canónico**: cada capacidad tiene una implementación responsable, contratos versionados y pruebas propias.
6. **Integrar antes que reinventar**: preferir proyectos maduros cuando su licencia, seguridad, compatibilidad y mantenimiento lo permitan.
7. **Trazabilidad completa**: toda intervención registra evidencia, decisiones, aprobaciones, operaciones y verificaciones, sin almacenar secretos.
8. **Mínimo privilegio**: no elevar privilegios ni modificar el equipo cuando no sea necesario.
9. **Reversibilidad**: respaldos, comprobaciones previas, planes de recuperación y validación posterior antes de reparar.
10. **Portabilidad verificable**: artefactos reproducibles, manifiestos, hashes, firmas y matriz explícita de compatibilidad.

## Decisiones arquitectónicas iniciales

- **Go**: núcleo de ejecución, CLI, servicios locales, orquestación, contratos, auditoría y adaptadores.
- **Wails v3**: contenedor de escritorio para Field y herramientas de escritorio compatibles.
- **TypeScript + React**: interfaz de usuario compartida.
- **Python**: motor canónico de analítica numérica, estadística y modelos; se incorpora cuando exista una capacidad analítica real que lo requiera.
- **PowerShell y herramientas nativas**: adaptadores del sistema operativo; no contienen reglas de negocio duplicadas.
- **SQLite**: almacenamiento local/portable, con el driver sin CGO `modernc.org/sqlite`.
- **PostgreSQL**: almacenamiento multiusuario de Hub.
- **JSON Schema / OpenAPI**: contratos versionados entre componentes y lenguajes.
- **Ollama**: adaptador opcional para modelos locales.

## Estado del proyecto

**Etapa global: fundación arquitectónica y núcleo Go inicial.** SOPTRAZ es un producto en desarrollo, no una versión estable. La CLI y los componentes locales priorizan lectura, contratos, auditoría, validación y límites de seguridad. El perfil de dispositivo es read-only; todavía no hay UI Field/Hub terminada ni acciones de reparación mutantes habilitadas.

**M1.4 permanece abierto.** La ruta online oficial, el trust store SQLite, las generaciones y el snapshot de lectura consistente están implementados. La verificación offline estricta, el resolver de delegaciones para uso offline, la política temporal persistida, el bootstrap productivo de la root y la aceptación final siguen pendientes. El detalle vivo y el orden correcto están en [Trabajo activo y continuidad](docs/roadmap/active-work.md), que prevalece sobre listas antiguas o comentarios de PR.

**Separación de responsabilidades:** el catálogo autentica metadata y descriptores; no descarga, instala ni ejecuta binarios. M1.5 debe aportar un supervisor seguro y separado antes de que SOPTRAZ pueda ejecutar herramientas externas. No se debe presentar una caché SQLite válida estructuralmente como confianza criptográfica ni afirmar protección anti-rollback fuerte frente a la restauración integral de un disco sin un ancla externa/monotónica.

## Diagnóstico local del reloj

La CLI permite consultar las señales horarias que expone el sistema operativo:

```sh
go run ./cmd/soptraz time-status
go run ./cmd/soptraz time-status --json
```

El comando es de solo lectura. Distingue estado del servicio y estado de sincronización; cuando la plataforma no ofrece una señal segura, informa `unknown` o `unavailable`. La hora observada y el estado del servicio son datos operativos, no evidencia temporal criptográfica ni autorización para verificar metadata TUF expirada.

## Documentación

- [Visión y alcance](docs/product/vision.md)
- [Arquitectura general](docs/architecture/overview.md)
- [Selección tecnológica](docs/architecture/technology-strategy.md)
- [Catálogo autenticado TUF (ADR-0015)](docs/decisions/ADR-0015-tuf-artifact-catalog.md)
- [Almacén durable de confianza TUF (ADR-0017)](docs/decisions/ADR-0017-tuf-sqlite-trust-store.md)
- [Optimización y mantenimiento modular de Windows (ADR-0016)](docs/decisions/ADR-0016-windows-optimization.md)
- [Perfil adaptativo de dispositivo Windows](docs/architecture/windows-device-profile.md)
- [Límites y contratos de módulos](docs/architecture/module-contracts.md)
- [Modelo de auditoría](docs/architecture/audit-model.md)
- [Diseño de checkpoints firmados y exportación Field → Hub](docs/architecture/audit-checkpoint-design.md)
- [Field y Hub](docs/architecture/editions.md)
- [Política de IA](docs/architecture/ai-policy.md)
- [Estrategia de integración](docs/architecture/integration-strategy.md)
- [Contratos ejecutables](docs/contracts/README.md)
- [Calidad y pruebas](docs/quality/testing-strategy.md)
- [Revisión de fallos de CI](docs/quality/ci-failure-review.md)
- [Checkpoint firmado y exportación Field → Hub](docs/architecture/audit-checkpoint-design.md)
- [Trabajo activo y guía de continuidad (punto de entrada del desarrollador)](docs/roadmap/active-work.md)
- [ADR-0019 — Verificación offline explícita del catálogo TUF](docs/decisions/ADR-0019-offline-tuf-catalog-verifier.md)
- [ADR-0020 — Tiempo confiable y recuperación del reloj](docs/decisions/ADR-0020-trusted-time-and-clock-recovery.md)
- [ADR-0021 — Diagnóstico horario del sistema operativo](docs/decisions/ADR-0021-os-time-diagnostics.md)
- [Roadmap](docs/roadmap/README.md)
- [Registro de decisiones](docs/decisions/README.md)
- [Contribución](CONTRIBUTING.md)
- [Seguridad](SECURITY.md)

## Estado de calidad

No se declara ninguna capacidad como estable hasta contar con pruebas automatizadas, evidencia de compatibilidad y documentación. Las herramientas de terceros conservan su identidad, licencia y ciclo de actualización; una integración no implica que SOPTRAZ sea autor o garante del software externo.
