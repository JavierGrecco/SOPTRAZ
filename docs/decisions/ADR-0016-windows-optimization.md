# ADR-0016: Optimización y mantenimiento modular de Windows

- Estado: Aceptada como dirección de producto y arquitectura; implementación pendiente.
- Fecha: 2026-11-27
- Relacionada: ADR-0006, ADR-0007, ADR-0010, ADR-0015, Issue #9 (M1.6), Issue #5 (M1.4), Issue #4 (M1.5).

## Contexto

SOPTRAZ debe cubrir no solo diagnóstico y reparación, sino también la puesta a punto de equipos Windows: inventario, mantenimiento, energía, aplicaciones de inicio, privacidad, almacenamiento, gaming, drivers y accesos rápidos. MiniOS/DELTA muestran una experiencia de módulos y menú extendido; WinUtil y Sophia Script aportan catálogos de cambios; WinGet y Sysinternals cubren instalación e inspección.

El riesgo es convertir SOPTRAZ en un agregador de scripts de tweaks con resultados opacos. Los cambios de registro, servicios, tareas, paquetes y arranque pueden afectar seguridad, actualizaciones, conectividad, funciones del usuario o estabilidad. Además, un valor por defecto documentado por un proyecto externo no necesariamente coincide con el valor previo real del equipo.

## Alternativas

1. **Copiar un toolkit completo o ejecutar presets externos directamente:** se descarta. Mezcla decisiones de terceros, shell y privilegios, dificulta medir compatibilidad y rollback, y obliga a heredar cambios o políticas que SOPTRAZ no controla.
2. **Distribuir un Windows modificado o depender de DELTA/MiniOS:** se descarta como base del producto. La distribución de un sistema modificado no es equivalente a configurar una instalación existente; no se ha verificado una licencia que permita reutilizar o redistribuir los ejecutables/código de MiniOS/DELTA.
3. **Reimplementar todos los tweaks desde cero:** se descarta como estrategia general. Duplica trabajo ya existente y aumenta el riesgo de mantener políticas de Windows sin una fuente de referencia.
4. **Catálogo propio, declarativo y auditable; adaptación selectiva de proyectos maduros:** seleccionada. SOPTRAZ conserva contratos, política de riesgo, inventario de estado, aprobación, verificación y restauración. Cada implementación externa se evalúa por separado según licencia, compatibilidad, mantenimiento y pruebas.

## Decisión

1. Crear la vertical **Optimización y mantenimiento modular de Windows (M1.6)**. Depende de M1.4 (catálogo autenticado) y M1.5 (supervisor seguro) para ejecutar cualquier acción mutante. El trabajo de inventario, recomendaciones y baseline de solo lectura puede adelantarse dentro de Fase 2 sin habilitar modificaciones.
2. El núcleo no será un catálogo de comandos arbitrarios. Cada capacidad estará definida como acción declarativa con ID/versión, descripción, sistemas/builds/ediciones/arquitecturas admitidos, precondiciones, entradas tipadas, capacidades, privilegios, riesgo, duración máxima, idempotencia, efecto esperado, efectos secundarios, verificación posterior, procedencia, licencia y estrategia de restauración.
3. El motor capturará el **estado real previo del equipo inmediatamente antes del cambio**. No asumirá que un valor genérico como `OriginalValue` es el estado del usuario. La restauración comprobará que el estado actual sigue siendo el aplicado por SOPTRAZ; si detecta cambios externos, informará el conflicto y no sobrescribirá silenciosamente la configuración posterior.
4. Cada acción declarará `rollback: exact | partial | unavailable`. Las acciones parciales o irreversibles no formarán parte de perfiles recomendados. Los puntos de restauración de Windows son una protección adicional, no sustituyen la copia/registro del estado ni el rollback específico.
5. Los perfiles **Mínimo, Equilibrado, Gaming, Batería, Privacidad y Personalizado** serán conjuntos revisables de acciones compatibles con el equipo. SOPTRAZ mostrará las acciones excluidas y la razón (hardware, build, política, privilegios, riesgo o ausencia de rollback). No habrá aplicación silenciosa de un preset completo.
6. Las áreas funcionales candidatas son inventario/baseline; limpieza y mantenimiento; inicio/aplicaciones; servicios/funciones; energía; gaming/GPU; discos; red; drivers/librerías; privacidad, seguridad y personalización. Cada área se habilitará gradualmente con evidencia propia.
7. **Fuentes prioritarias para investigación/adaptación:**
   - WinUtil (`ChrisTitusTech/winutil`, MIT): estudiar presets, catálogo declarativo, logging, estado de tweaks y Undo. Su release revisada es `26.11.13`; la documentación vigente prioriza Windows 11. No importar presets completos: el catálogo incluye acciones con scripts, servicios y cambios que no comparten la misma reversibilidad.
   - Sophia Script (`farag2/Sophia-Script-for-Windows`, MIT): estudiar funciones pequeñas y pares Apply/Restore, conservando su matriz de versiones Windows 10/11, LTSC y ARM64. La release revisada es `8.1.0`; Windows 10 está congelado en una rama anterior.
   - WinGet (`microsoft/winget-cli`, MIT): proveedor de instalación declarativa desde fuentes oficiales. Las configuraciones de terceros se consideran código/entrada no confiable hasta revisar recursos, origen, versión, privilegios y acciones requeridas.
   - Microsoft Sysinternals: utilidades de diagnóstico/inventario con distribución y términos propios de Microsoft. Evaluar cada ejecutable, versión, firma, hash y condiciones de redistribución; no asumir que la suite puede empaquetarse dentro de SOPTRAZ.
   - Atlas-OS (`Atlas-OS/Atlas`, GPL-3.0): referencia de catálogo de toggles, estados, elevación, reinicios, inventario de binarios y comprobación posterior. No portar código sin revisar obligaciones copyleft y el impacto de su modelo de instalación/modificación profunda.
   - DProjects/MiniOS/DELTA: referencia de UX, menú extendido, perfiles y módulos Gaming Library. No se encontró un repositorio de código ni una licencia de reutilización verificable para los binarios/DELTA; se toma como inspiración funcional, no como dependencia ni contenido redistribuible.
8. No se incorporará una función de "limpieza de RAM" genérica. SOPTRAZ diagnostica presión de memoria, procesos y consumo; no vacía listas Standby ni mata procesos a ciegas para presentar memoria libre como rendimiento ganado.
9. No incluir como acciones automáticas de perfiles recomendados la desactivación de Defender, Firewall, UAC, Windows Update, BitLocker, TPM, Secure Boot, VBS, Memory Integrity, mitigaciones de CPU, servicios en masa, BCD/arranque, ni overclock/voltajes. Los ajustes de red avanzados (MAC/MTU/DNS) requieren diagnóstico y verificación que demuestre pertinencia; no se presume que reduzcan latencia o mejoren FPS.
10. Las acciones externas se resolverán mediante M1.4 y solo serán ejecutadas por M1.5: ejecutable firmado y fijado, argumentos tipados sin shell, entorno mínimo, privilegios explícitos, timeout, cancelación, límites de salida y auditoría. No usar descargas ejecutadas directamente, exclusiones antivirus ni scripts remotos sin pin.
11. La interfaz de menú rápido/contextual es una capa futura de Field. Solo invocará IDs de acciones registradas; no contendrá lógica de negocio, PowerShell ni cambios de sistema embebidos en handlers del menú.

## Consecuencias

- SOPTRAZ puede ofrecer una experiencia amplia de puesta a punto sin convertirse en una distribución de Windows modificada ni depender de un único toolkit.
- La adaptación de proyectos externos será granular y conservará origen, licencia, versión, hash y soporte por plataforma.
- Las acciones no se anunciarán como "mejora de FPS" salvo medición reproducible antes/después, con hardware, drivers, build, juego y metodología documentados.
- Las VMs/snapshots, las pruebas de reinicio y restauración, y la detección de cambios externos son requisitos para habilitar acciones mutantes.
- El menú extendido y los perfiles no se implementarán antes de disponer de acciones con contratos, verificación y supervisor seguros.

## Entregas y aceptación

1. Catálogo/contrato declarativo de acciones Windows con matriz de compatibilidad y restauración.
2. Baseline e informe de inventario de solo lectura, sin elevación innecesaria.
3. Adaptación inicial y auditada de una acción de bajo riesgo y reversible, después de M1.5.
4. Perfiles que solo agrupen acciones compatibles y muestren todos los cambios previstos antes de aprobarlos.
5. Pruebas en VMs para Windows 10/11 y builds/ediciones/arquitecturas declaradas; cubrir reinicio, conflictos con cambios externos, fallo parcial y rollback.
6. Benchmark reproducible para toda afirmación de rendimiento; sin promesas universales de FPS, latencia o memoria.
7. Revisión de licencias y condiciones de redistribución por cada archivo/código/binario integrado.


## Investigación específica de MiniOS y MENU Extendido

La investigación detallada del ecosistema DProjects, del menú contextual, de la distribución de utilidades de terceros y del enfoque propuesto para SOPTRAZ se mantiene en [docs/research/minios-and-context-menu.md](../research/minios-and-context-menu.md). Distingue declaraciones del proveedor, mecanismos de Windows documentados e hipótesis no verificadas; no habilita la distribución de imágenes o binarios de terceros.
