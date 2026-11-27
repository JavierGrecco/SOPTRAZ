# Investigación: MiniOS, DELTA y MENU Extendido

- Fecha de corte: 2026-11-27
- Relacionado: ADR-0016 e Issue #9 (M1.6)
- Estado: investigación documental; no se incorporan binarios ni código de terceros.

## 1. Resumen ejecutivo

MiniOS/DProjects no es una sola aplicación. La documentación pública describe un ecosistema que combina imágenes de Windows personalizadas, un asistente post-instalación (MiniOS Toolkit), una utilidad de escritorio, un paquete de runtimes para juegos y aplicaciones, una extensión del menú contextual (MENU Extendido) y herramientas complementarias. DELTA es un producto comercial separado, con módulos adicionales de optimización y configuración.

El valor más transferible a SOPTRAZ no es construir otra ISO recortada ni acumular ejecutables portables. Es ofrecer un punto de acceso organizado y fácil de descubrir para funciones de Windows, diagnósticos y utilidades especializadas.

Recomendación: construir un catálogo propio de herramientas y acciones, inicialmente dentro de SOPTRAZ Field, con funciones nativas de Windows y enlaces a los fabricantes. En una etapa posterior podremos agregar integración contextual opcional con Explorer. El menú invocará acciones registradas por SOPTRAZ; no contendrá comandos arbitrarios ni duplicará la lógica de optimización.

No se encontró código fuente público, un manifiesto completo de componentes o un inventario reproducible de todos los ajustes y utilidades de MiniOS/DELTA/MENU Extendido. Por eso separamos hechos publicados, inferencias técnicas y aspectos no verificables.

## 2. Qué productos forman el ecosistema

### 2.1 MiniOS y sus componentes

La FAQ oficial presenta MiniOS como un ecosistema gratuito de ajustes y optimizaciones para Windows, aplicados con MiniOS Toolkit y acompañado por estas utilidades:

| Componente | Función publicada | Lectura para SOPTRAZ |
|---|---|---|
| MiniOS Toolkit | Asistente gratuito que aplica ajustes y optimizaciones a un Windows instalado | Referencia de flujo guiado, no dependencia ejecutable |
| MiniOS Desktop | Monitorización en tiempo real, limpieza, gestión de RAM y reparación | Referencia para una consola de estado; separar monitorización útil de supuestos beneficios de liberar RAM |
| Gaming Library | Paquete con DirectX, Visual C++, .NET, vcruntime, msvcp, xinput y d3dx | Resolver runtimes faltantes desde paquetes oficiales adecuados |
| MENU Extendido | Extensión del menú contextual de Windows con accesos rápidos y utilidades | Referencia directa para una experiencia de accesos agrupados |
| Fortnite Cleaner | Limpieza de caché y residuos específicos de Fortnite | Ejemplo de utilidad acotada a una aplicación |

La FAQ también enumera otras herramientas del ecosistema, como lanzador, captura y monitorización. Son productos separados: no deben confundirse con Toolkit ni con MENU Extendido.

### 2.2 DELTA

DELTA es un producto comercial separado de MiniOS Toolkit. Su página oficial describe un asistente de post-instalación para Windows 10/11 con módulos orientados especialmente a gaming, personalización y puesta a punto. La página enumera:

- Detección del tipo de equipo, GPU y unidades.
- Módulo GPU y perfiles para hardware NVIDIA, AMD e Intel.
- Ajustes de CPU y determinados servicios.
- Instalación de librerías como DirectX y Visual C++.
- Gestión y optimización de SSD, M.2 y HDD.
- Compresión de Windows.
- Gestión selectiva de componentes opcionales y opciones de privacidad, Copilot, telemetría e IA.
- Personalización y migración de edición.
- Niveles progresivos de cambios (Safe, Medium y Extreme, según la FAQ).

La propia información de DProjects advierte que los resultados dependen del hardware y no garantiza un aumento universal de FPS. Una colección de toggles no equivale a una mejora medible y un perfil Gaming no debe aplicar cambios solo por su nombre.

### 2.3 Imágenes MiniOS

DProjects publica distintas ediciones de MiniOS construidas sobre diferentes versiones y ediciones de Windows. Su documentación explica diferencias entre bases y advierte que las variantes más agresivas, como MiniOS X, pueden recortar componentes y afectar la compatibilidad con aplicaciones, controladores o juegos.

Esto es distinto de ejecutar un asistente sobre el Windows original del usuario. Una ISO modificada cambia el punto de partida del sistema: componentes disponibles, mantenimiento, compatibilidad, reparación, servicing y soporte. SOPTRAZ no debería distribuir ni depender de una imagen de Windows modificada.

## 3. Qué mejora y cómo lo hace: hechos vs. lo que no podemos confirmar

### 3.1 Funciones que DProjects declara

La documentación del proveedor atribuye al ecosistema estas áreas:

- Menos componentes o aplicaciones no deseadas, de forma selectiva.
- Ajustes de CPU/GPU y configuración para juegos.
- Instalación de runtimes y librerías que necesitan juegos y aplicaciones.
- Ajustes de almacenamiento y compresión.
- Opciones de privacidad, telemetría, Copilot/IA y personalización.
- Limpieza de archivos/cachés y accesos a herramientas de reparación.
- Monitorización del equipo (CPU, GPU, memoria y otros indicadores, según la utilidad).

Son descripciones del fabricante, no evidencia independiente de que cada ajuste mejore todos los equipos. Un cambio puede beneficiar un escenario y degradar otro: quitar una característica puede reducir espacio, pero romper una aplicación o función que el usuario necesita.

### 3.2 Mecanismos técnicos documentados de Windows

Microsoft documenta herramientas para modificar imágenes Windows sin arrancarlas, por ejemplo DISM sobre imágenes WIM/VHD/FFU montadas. Estas herramientas permiten realizar servicing de imágenes, administrar paquetes, controladores y características según el escenario.

Esto explica una familia de mecanismos que un fabricante de una ISO personalizada podría usar, pero **no demuestra que DProjects use exactamente DISM, qué comandos ejecuta ni qué paquetes elimina**. Tampoco permite deducir claves de registro, servicios o tareas concretas.

En un Windows ya instalado, un optimizador puede actuar mediante políticas, configuración, registro, servicios, paquetes aprovisionados/opcionales, tareas, energía, aplicaciones de inicio y utilidades del sistema. Sin código fuente, manifiestos, logs de ejecución o pruebas comparativas de MiniOS no podemos atribuirle un mecanismo específico a cada acción.

### 3.3 Qué no pudimos verificar

No localizamos públicamente:

- Código fuente del Toolkit, DELTA o MENU Extendido.
- Manifiesto completo y versionado de todos los ajustes.
- Inventario exacto de binarios de terceros incluidos en cada versión del menú.
- Lista completa de cambios de Registro/servicios/tareas por perfil.
- Snapshot real de valores anteriores o implementación de Undo.
- Proceso detallado de compatibilidad por build, edición y controlador.
- Benchmarks reproducibles, hardware de referencia y condiciones de prueba por ajuste.

**Conclusión:** no corresponde inventar una explicación de cómo MiniOS lo hace internamente. Podemos estudiar los flujos de producto, módulos anunciados y experiencia; para conocer cambios exactos sería necesario analizar una versión aislada y sus efectos, sin instalarla en equipos productivos ni ejecutar archivos de procedencia no verificada. No necesitamos reproducir ese análisis para diseñar nuestra arquitectura.

## 4. MENU Extendido: qué sabemos y qué podemos aprender

### 4.1 Qué es

DProjects define MENU Extendido como una extensión del menú contextual de Windows que agrega accesos rápidos y utilidades. Su catálogo de descargas lo publica como herramienta separada y muestra versiones de ese producto.

En una captura promocional del ecosistema aparece una jerarquía de categorías similar a:

- Limpieza
- Sistema
- Varias
- Sordum
- Discos
- Gamer
- Redes
- Drivers
- Información del sistema
- Reinicio/apagado

La captura demuestra el patrón de menú contextual agrupado, pero **no es un inventario verificable de todas las entradas de MENU Extendido v10.0**. No pudimos confirmar que cada categoría y herramienta de la imagen pertenezca exactamente a la versión independiente del menú, ni qué ejecutables concretos hay detrás de cada opción.

### 4.2 Qué hace bien la idea

Su ventaja de UX es reducir el coste de encontrar herramientas dispersas en Windows:

- Jerarquía pequeña de categorías en lugar de accesos desordenados.
- Vocabulario orientado a tareas: discos, red, limpieza, drivers, sistema.
- Acceso contextual desde Explorer, donde el usuario ya está trabajando.
- Descubrimiento de utilidades que muchos usuarios no conocen.
- Separación visual entre herramientas generales y especializadas.

SOPTRAZ puede capturar esta ventaja sin copiar menú, código o paquetes. La experiencia será propia y conectará con el catálogo canónico de acciones.

### 4.3 Cómo implementarlo con menor riesgo

Windows contempla verbos y extensiones para menús contextuales. También existen verbos extendidos que pueden mostrarse solo al mantener Shift mientras se hace clic derecho. En Windows 11, las extensiones modernas de Explorer tienen un modelo específico basado en IExplorerCommand y registro con identidad de aplicación.

Hay un riesgo técnico importante: algunas extensiones se cargan dentro de Explorer y pueden afectar al propio proceso. La documentación de Microsoft advierte que los controladores de menú contextual pueden cargarse en procesos anfitriones y causar problemas allí si están mal implementados.

Propuesta por etapas:

1. **Primero un lanzador dentro de SOPTRAZ Field:** panel rápido, buscable y organizado por categorías. No modifica Explorer y permite probar taxonomía y telemetría.
2. **Después un único acceso contextual liviano:** abrir SOPTRAZ Toolbox, sin operaciones complejas ni trabajo pesado dentro del callback de Explorer.
3. **Por último, menú avanzado opcional:** acceso oculto con Shift+clic derecho o submenú moderno compatible con Windows 11, solo después de pruebas de instalación, actualización y desinstalación.
4. El menú solo presenta funciones y envía un action_id al runtime. No alberga PowerShell, scripts, comandos libres ni lógica duplicada.

## 5. Programas y utilidades de terceros: licencias, peso y distribución

### 5.1 La regla principal

Que una utilidad sea gratuita, portable o descargable no significa que SOPTRAZ pueda reempaquetarla, cambiarle nombre o iconos, alojarla, incorporarla a un producto comercial o ejecutarla con privilegios sin intervención.

Hay que distinguir cuatro modelos:

| Modelo | Qué hace SOPTRAZ | Política recomendada |
|---|---|---|
| Función nativa de Windows | Abre Settings, MMC, herramientas administrativas o consulta APIs nativas | Primera opción; evita peso y licencias externas |
| Enlace al fabricante | Abre la web oficial del proyecto | Apto cuando no hay integración aprobada |
| Instalación bajo demanda | Invoca WinGet o el instalador del publisher tras revisar package ID, fuente, publisher, versión y consentimiento | Apto con validación y auditoría |
| Distribución incluida | Empaqueta el ejecutable en el instalador de SOPTRAZ | Solo con permiso/licencia explícita y revisión de obligaciones |

### 5.2 El ejemplo de Sordum

La licencia de Sordum permite redistribuir ciertas copias freeware solo en forma exacta y sin modificaciones, con enlace directo a la descarga o página original y licencia incluida. También prohíbe incorporar sus productos en un producto vendido sin autorización previa.

Como la captura del menú muestra una categoría Sordum, es un buen ejemplo de por qué debemos revisar cada proveedor. No pudimos confirmar qué herramientas de Sordum se distribuyen dentro del menú ni si el acuerdo específico de MENU Extendido contempla otras condiciones. Por tanto, no debemos empaquetarlas por inferencia.

### 5.3 Windows e imágenes modificadas

Las condiciones de licencia de Windows y las reglas de reimagen de Microsoft no ofrecen una autorización general para que un tercero publique libremente imágenes modificadas. Los derechos de reimagen aplican a licencias y escenarios concretos.

SOPTRAZ debe trabajar sobre la instalación licenciada por el usuario o el fabricante; no debe descargar, alojar, redistribuir ni construir una ISO de Windows alterada como parte de esta vertical.

### 5.4 WinGet y paquetes

WinGet puede ser un proveedor de instalación, no una garantía absoluta de que cada paquete o manifiesto sea seguro. Microsoft documenta configuraciones YAML reproducibles, pero también exige revisar la confiabilidad de la configuración porque instala software y cambia el estado del equipo.

Antes de instalar, SOPTRAZ deberá comprobar:

- ID exacto del paquete y origen.
- Publisher declarado frente a la firma del instalador.
- Versión fijada, arquitectura y URL oficial.
- Hash y firma digital, cuando existan.
- Licencia y condiciones de redistribución.
- Requisitos y cambios del instalador.
- Consentimiento explícito del usuario.
- Resultado y versión instalada registrados en auditoría.

No utilizar métodos de descarga y ejecución directa como irm-pipe-iex o curl-pipe-shell, scripts remotos improvisados, mirrors desconocidos ni exclusiones antivirus para forzar una ejecución.

### 5.5 Peso del instalador

El menú y sus metadatos son livianos; los ejecutables, runtimes y paquetes de terceros son los que hacen crecer el producto y generan mantenimiento.

La estrategia recomendada:

- Mantener pequeño el núcleo SOPTRAZ.
- No empaquetar suites grandes de terceros.
- Instalar herramientas opcionales solo bajo demanda.
- Descargar desde la fuente oficial.
- Cachear solo artefactos con manifiesto autenticado y política de retención.
- Permitir desinstalar complementos y limpiar cachés con trazabilidad.
- No duplicar runtimes de Visual C++, .NET o DirectX si ya están presentes.
- Evitar DLL sueltas obtenidas de sitios de descarga; usar redistribuibles oficiales y versionados.

Esto reduce tamaño, riesgos de supply chain, obligaciones legales, falsas alertas antivirus, actualizaciones duplicadas y dependencia del mantenimiento de terceros.

## 6. Diseño propuesto: SOPTRAZ Toolbox

### 6.1 Categorías iniciales

La primera versión debe centrarse en enlaces y diagnósticos read-only nativos de Windows:

| Categoría | Herramientas/funciones iniciales |
|---|---|
| Sistema | Información del sistema, Administrador de tareas, Visor de eventos, Monitor de recursos, Monitor de confiabilidad, Administrador de dispositivos, servicios en modo inspección |
| Limpieza | Configuración de almacenamiento, Sensor de almacenamiento, Papelera, previsualización de temporales y cachés con exclusiones |
| Discos | Administración de discos, Optimizar unidades, inventario de volúmenes, SMART de lectura |
| Red | Configuración de red, adaptadores, diagnóstico Wi-Fi/Ethernet, latencia, pérdida de paquetes y resolución DNS |
| Gaming | Modo Juego, configuración gráfica por aplicación, detección de GPU y benchmark antes/después |
| Drivers y librerías | Inventario de proveedor/versión/firma, enlaces OEM/GPU, detección de runtimes instalados |
| Seguridad | Estado de Defender, Firewall, BitLocker, TPM, Secure Boot, VBS, Memory Integrity y Windows Update; lectura por defecto |
| Herramientas externas | Catálogo opt-in con publisher, licencia, versión, tamaño, origen, firma y método de instalación |

Los nombres concretos son una propuesta funcional. Cada entrada debe tener una prueba de disponibilidad y un comportamiento alternativo por edición/build.

### 6.2 Metadatos obligatorios de cada entrada

Cada herramienta o acción declarará:

- tool_id/action_id, versión y categoría.
- Tipo de destino: recurso nativo, enlace oficial, paquete WinGet o acción SOPTRAZ.
- Sistemas/builds/arquitecturas compatibles.
- Solo lectura o mutación.
- Privilegios, riesgo, impacto y confirmación necesaria.
- Origen, publisher, firma, hash, licencia y tamaño si es externa.
- Timeout, idempotencia, verificación y clasificación de rollback.
- Estado actual, requisitos faltantes y motivo de deshabilitación.

Las acciones mutantes continúan bloqueadas hasta completar M1.4 y M1.5. El menú avanzado no debe convertirse en un atajo para saltar las puertas de seguridad.

### 6.3 Qué no vamos a copiar

- ISOs o builds de Windows personalizadas.
- Ejecutables de MiniOS, DELTA o MENU Extendido.
- Herramientas freeware o comerciales sin permiso explícito de redistribución.
- Presets opacos de tweaks externos.
- Paquetes masivos de drivers.
- Herramientas que pidan desactivar antivirus o crear exclusiones para funcionar.
- Pausas indefinidas/desactivación de Windows Update.
- Limpiadores genéricos de RAM, servicios deshabilitados en masa o promesas universales de FPS.

## 7. Matriz de decisión

| Elemento de MiniOS/DProjects | Interés para SOPTRAZ | Decisión |
|---|---:|---|
| Idea de módulos por tarea | Alto | Adoptar como principio UX |
| Menú contextual agrupado | Muy alto | Diseñar Toolbox propio; Explorer después |
| Accesos a funciones nativas de Windows | Muy alto | Implementar primero |
| Detección automática de hardware y componentes | Alto | Implementar read-only en Fase 2 |
| Perfiles de tweaks | Alto | Catálogo declarativo y perfiles revisables; no copiar presets |
| Gaming Library | Medio/alto | Detectar y resolver runtimes oficiales solo cuando falten |
| MiniOS Desktop (monitor/limpieza/RAM) | Medio | Estudiar sensores; no adoptar “limpieza de RAM” genérica |
| MiniOS/DELTA binaries | Nulo como dependencia inicial | No reempaquetar ni ejecutar |
| ISO Windows personalizada | No alineado | No distribuir ni depender |
| Utilidades externas integradas | Variable | Enlaces o instalación bajo demanda, con licencia y firma verificadas |

## 8. Plan incremental de SOPTRAZ

1. Investigación y arquitectura: documentar ecosistema, límites de evidencia, licencias, categorías y experiencia de menú.
2. Catálogo declarativo read-only: definir esquema de Toolbox y primeras entradas nativas. Sin scripts arbitrarios ni tweaks.
3. Inventario y baseline de Windows: build, edición, arquitectura, hardware, discos, drivers, energía, seguridad e inicio.
4. M1.4: completar trust root, rotación, anti-rollback, persistencia y política offline.
5. M1.5: supervisor de procesos restringido, argumentos tipados, límites, cancelación y auditoría.
6. M1.6: solo tras las puertas anteriores, acciones reversibles individuales y perfiles revisables.
7. Integración Explorer: cuando Toolbox esté estable y haya pruebas de instalación, actualización, desinstalación y recuperación de Explorer en Windows 10/11 y x64/ARM64.

## 9. Fuentes y alcance de la investigación

### Fuentes oficiales consultadas

- DProjects, [Preguntas frecuentes](https://dprojects.org/faq/) — definición del ecosistema MiniOS, Toolkit, Desktop, Gaming Library y MENU Extendido.
- DProjects, [DELTA](https://dprojects.org/delta/) — módulos y funciones anunciadas para el producto comercial.
- DProjects, [Centro de descargas](https://dprojects.org/descargas/) — catálogo y flujo de distribución.
- DProjects, [Versiones de MiniOS](https://dprojects.org/docs/versiones-minios/) — bases de Windows y advertencias de compatibilidad.
- Microsoft Learn, [Modificar una imagen de Windows mediante DISM](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/mount-and-modify-a-windows-image-using-dism?view=windows-11).
- Microsoft Learn, [Controladores de menú contextual](https://learn.microsoft.com/en-us/windows/win32/shell/context-menu-handlers).
- Microsoft Learn, [Extender el menú contextual moderno de File Explorer](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/integrate-packaged-app-with-file-explorer).
- Microsoft Learn, [WinGet Configuration](https://learn.microsoft.com/en-us/windows/package-manager/configuration/).
- Microsoft, [Reimaging Rights](https://www.microsoft.com/licensing/guidance/Reimaging-rights).
- Sordum, [EULA](https://www.sordum.org/eula/).

### Límites de verificación

Las páginas consultadas documentan capacidades y declaraciones del proveedor, pero no proporcionan el código fuente o el inventario ejecutable completo de MiniOS. La revisión no ejecutó ni instaló imágenes o binarios de terceros. Las categorías observadas en capturas se tratan como referencia visual, no como catálogo completo y probado de MENU Extendido. Tampoco se tomó como evidencia independiente ninguna promesa comercial de FPS o rendimiento.

## 10. Decisión propuesta para ADR-0016

Adoptar la experiencia de Toolbox agrupada y contextual como capacidad propia de SOPTRAZ, manteniendo separadas presentación, catálogo de acciones y ejecución. La primera versión utilizará herramientas nativas y diagnósticos read-only. Los complementos de terceros serán enlaces o instalaciones bajo demanda desde fuentes verificadas. El menú de Explorer será opcional y posterior, y toda acción que modifique Windows seguirá condicionada a M1.4/M1.5.

Esta decisión incorpora el valor funcional que observamos en MiniOS sin heredar sus binarios, licencias, ISO, presets ni cadena de mantenimiento.


## 11. Segunda verificación del catálogo y comparativa Toolkit vs. DELTA

La comparativa oficial de DProjects permite concretar mejor la frontera entre los productos:

| Capacidad publicada | MiniOS Toolkit | DELTA |
|---|---|---|
| DirectX, Visual C++ y .NET | Sí, automático | Sí, automático |
| Instalar software (navegadores, librerías y programas) | No indicado en la comparativa | Sí |
| MENU Extendido | Versión normal | Versión avanzada |
| Reparación de DLL faltantes en juegos | Sí | Sí |
| Personalización de escritorio/íconos/temas | Básica | Avanzada |
| Módulos especializados | Limitados | Más de 12 |
| Tweaks de GPU para NVIDIA/AMD/Intel | No indicado como función Toolkit | Sí |
| Tweaks de CPU, servicios y prioridades | No indicado como función Toolkit | Sí |
| Optimización SSD/M.2/HDD y compresión | No indicado como función Toolkit | Sí |
| Debloat selectivo de Copilot/telemetría/IA | No indicado como función Toolkit | Sí |

DProjects caracteriza ambos como asistentes de post-instalación de una sola sesión, con DELTA como opción más profunda. Su página describe detección de portátil/sobremesa, GPU y discos; muestra los módulos y automatiza la aplicación en un flujo guiado. La comparativa es una declaración del proveedor, no un inventario de código ni una validación independiente de seguridad o reversibilidad.

### Catálogo público a la fecha de corte

En la página pública de descargas se listan, entre otros, Gaming Library v5.0, MENU Extendido v10.0, MonitorHUB v1.0.76, Replay v1.0.49, StartOne v1.0.69, Krenshot v1.0.72, Fortnite Cleaner v2.0 y NoUpdates v1.0.15. Esto es información del catálogo en la fecha de corte, no una certificación de autenticidad de los binarios ni prueba de qué herramientas exactas trae MENU Extendido.

La misma página describe NoUpdates como una herramienta que puede activar, desactivar o pausar Windows Update incluso hasta 2099. SOPTRAZ debe excluir esta capacidad de perfiles recomendados y no ofrecer la desactivación permanente de actualizaciones. Las actualizaciones de seguridad no son un bloatware que debamos eliminar.

La página de descargas también recomienda a sus usuarios crear una excepción temporal del antivirus si este marca herramientas propias de DProjects. Esa recomendación no es una señal concluyente de que el archivo sea malicioso, pero SOPTRAZ **no la adoptará**: no pediremos desactivar protección ni agregar exclusiones para ejecutar complementos. Un archivo que no pueda verificarse se bloquea y se informa.

El catálogo indica que el menú y las aplicaciones de DProjects son compatibles con Windows 10 21H2 o posterior y Windows 11. SOPTRAZ mantendrá su propia matriz de soporte basada en pruebas, y solo dará soporte a instalaciones de Windows con licencia válida y procedencia conocida; no tomará como objetivo la compatibilidad con imágenes no oficiales.

