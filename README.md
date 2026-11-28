# SOPTRAZ

**Intelligent IT Operations** — plataforma modular de diagnóstico, soporte, reparación, recuperación y gestión de operaciones IT.

SOPTRAZ se diseña como un producto propio que aprovecha herramientas y proyectos maduros. Su núcleo coordina módulos especializados, procedimientos verificables e integraciones; no intenta reimplementar herramientas que ya resuelven bien una necesidad concreta.

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

**Etapa: fundación arquitectónica y núcleo inicial.** La CLI Go permite consultar versión, información no identificable del host, validar manifiestos, acciones, intervenciones, eventos, checkpoints y descriptores de artefactos, y generar un informe local de perfil del dispositivo mediante `soptraz device-profile [--json]`. Este inventario inicial es de solo lectura: consulta chasis SMBIOS, energía/batería, dispositivos touch/pen y estado convertible/dock cuando está disponible; conserva la incertidumbre como `unknown` y no aplica optimizaciones. El resolver local verifica archivos ya instalados bajo una raíz confinada. El catálogo TUF ya dispone de una primera capa online que autentica targets delegados; el bootstrap del root de producción y la persistencia offline integrada siguen pendientes. La persistencia de root-chain está integrada en `main` (PR #22 y #23): `NewPersistentTUFClient` reproduce las raíces desde el pin bootstrap con go-tuf y guarda las rotaciones aceptadas aun cuando falle metadata posterior. Faltan el bootstrap de producción, las generaciones completas con timestamp/snapshot/targets/delegaciones, high-water por rol y el validador offline anti-rollback. El catálogo aún no descarga ni ejecuta herramientas externas. El dominio de auditoría y su adaptador SQLite permiten anexar y verificar eventos encadenados por intervención; el decodificador JSON compartido rechaza claves duplicadas, UTF-8 inválido, escapes UTF-16 aislados, documentos concatenados y anidamiento excesivo, y el fuzzing comprueba la idempotencia del payload de auditoría. El primer bloque de checkpoint Ed25519 está implementado con resolución de claves mediante interfaz externa; la custodia real de claves y el anclaje remoto siguen pendientes. Todavía no hay reparaciones de sistemas ni UI de Field/Hub.

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
- [Roadmap](docs/roadmap/README.md)
- [Registro de decisiones](docs/decisions/README.md)
- [Contribución](CONTRIBUTING.md)
- [Seguridad](SECURITY.md)

## Estado de calidad

No se declara ninguna capacidad como estable hasta contar con pruebas automatizadas, evidencia de compatibilidad y documentación. Las herramientas de terceros conservan su identidad, licencia y ciclo de actualización; una integración no implica que SOPTRAZ sea autor o garante del software externo.
