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
- **SQLite**: almacenamiento local/portable.
- **PostgreSQL**: almacenamiento multiusuario de Hub.
- **JSON Schema / OpenAPI**: contratos versionados entre componentes y lenguajes.
- **Ollama**: adaptador opcional para modelos locales.

## Estado del proyecto

**Etapa: fundación arquitectónica y contratos iniciales.** La CLI Go permite consultar versión, información no identificable del host y validar manifiestos, acciones, intervenciones y eventos JSON. Estas capacidades no ejecutan reparaciones ni modifican el equipo.

## Documentación

- [Visión y alcance](docs/product/vision.md)
- [Arquitectura general](docs/architecture/overview.md)
- [Selección tecnológica](docs/architecture/technology-strategy.md)
- [Límites y contratos de módulos](docs/architecture/module-contracts.md)
- [Field y Hub](docs/architecture/editions.md)
- [Política de IA](docs/architecture/ai-policy.md)
- [Estrategia de integración](docs/architecture/integration-strategy.md)
- [Contratos ejecutables](docs/contracts/README.md)
- [Calidad y pruebas](docs/quality/testing-strategy.md)
- [Roadmap](docs/roadmap/README.md)
- [Registro de decisiones](docs/decisions/README.md)
- [Contribución](CONTRIBUTING.md)
- [Seguridad](SECURITY.md)

## Estado de calidad

No se declara ninguna capacidad como estable hasta contar con pruebas automatizadas, evidencia de compatibilidad y documentación. Las herramientas de terceros conservan su identidad, licencia y ciclo de actualización; una integración no implica que SOPTRAZ sea autor o garante del software externo.
