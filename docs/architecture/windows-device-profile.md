# Perfil adaptativo de dispositivo Windows

- Estado: arquitectura aceptada; clasificador puro y primer recolector nativo Windows de solo lectura integrados. Exposición CLI en revisión por PR #18. Sin acciones mutantes.
- Fecha: 2026-11-27
- Relacionado: ADR-0016, Issue #9, Issue #5 (M1.4), Issue #4 (M1.5), investigación MiniOS/DELTA.
- Principio: detectar el equipo y sus capacidades para adaptar recomendaciones; nunca inferir permiso para modificar Windows.

## 1. Objetivo

SOPTRAZ debe identificar si trabaja sobre un desktop, notebook, tablet, convertible/detachable, all-in-one, workstation, handheld o entorno virtual, y construir un perfil técnico que refleje las capacidades efectivamente presentes.

La categoría comercial no alcanza. Dos notebooks pueden diferir en batería, GPU, refrigeración, pantalla táctil, stylus, almacenamiento y memoria; una desktop puede tener pantalla touch; una convertible puede alternar entre teclado conectado y postura tablet. El motor debe razonar sobre capacidades detectadas y contexto dinámico, no aplicar un preset por el solo nombre del equipo.

El resultado será un **Device Profile** acompañado por evidencia, confianza, capacidades, limitaciones y recomendaciones. Los perfiles Mínimo, Equilibrado, Gaming, Batería, Privacidad y Personalizado se evaluarán contra ese perfil. La clasificación no ejecuta cambios ni eleva privilegios.

## 2. Separar cuatro conceptos

1. **Form factor físico:** desktop, laptop/notebook, tablet, convertible, detachable, all-in-one, workstation, handheld o desconocido.
2. **Capacidades:** batería, alimentación CA/CC, tapa, teclado físico, teclado conectado, touch, pen, sensores, GPU, unidades, memoria, red, TPM, Secure Boot, virtualización y sensores térmicos.
3. **Estado operativo dinámico:** conectado a CA o batería; teclado acoplado/desacoplado; dock conectado; temperatura/carga; ahorro de energía; sesión local/remota.
4. **Perfil de uso:** gaming, productividad, batería, diagnóstico, mantenimiento o personalizado. Es una preferencia/objetivo, no una propiedad deducida del chasis.

No convertir una característica en otra: tener pantalla táctil no convierte automáticamente una desktop en tablet; disponer de batería no prueba que un dispositivo sea una notebook (puede haber UPS u otros sistemas); “Mobile” de WMI no diferencia por sí solo un notebook moderno de una tablet.

## 3. Fuentes de detección de Windows

La detección se implementará mediante adaptadores nativos y consultas de solo lectura. Se registran fuente, valor, calidad, error y hora de observación para explicar la clasificación.

### 3.1 Identidad del chasis (SMBIOS)

Win32_SystemEnclosure.ChassisTypes aporta códigos procedentes del tipo de chasis SMBIOS. La documentación de Microsoft enumera, entre otros:

| Código SMBIOS | Interpretación |
|---:|---|
| 3–7 | Desktop / low profile / pizza box / mini tower / tower |
| 8 | Portable |
| 9 | Laptop |
| 10 | Notebook |
| 11 | Handheld |
| 13 | All-in-one |
| 30 | Tablet |
| 31 | Convertible |
| 32 | Detachable |

Fuente: Microsoft Learn, Win32_SystemEnclosure. Puede haber OEM que reporten valores genéricos, erróneos o múltiples; nunca será la única señal.

### 3.2 Tipo de sistema (WMI)

Win32_ComputerSystem.PCSystemType y PCSystemTypeEx identifican tipos generales como Desktop, Mobile, Workstation y Slate (según versión). Se usan como evidencia secundaria, no como verdad absoluta. En equipos antiguos o ciertos entornos el valor puede ser Unspecified.

### 3.3 Convertibilidad y postura

Windows documenta DeviceForm, Enclosure Type y ConvertibleSlateMode para que el hardware indique si es tablet/convertible/detachable y si el teclado físico está accesible. En Windows 11 22H2 o posterior, HKLM/System/CurrentControlSet/Control/PriorityControl/ConvertibilityEnabled, cuando existe, prevalece en la decisión de convertibilidad descrita por Microsoft. SOPTRAZ solo lee esta información: no cambia estas claves.

ConvertibleSlateMode aporta estado de teclado/postura, no cambia el form factor físico. En Windows 11 el modo Tablet clásico ya no está soportado; para cambios de teclado conectado/desconectado se debe utilizar Convertible Slate Mode cuando el fabricante lo implemente. No inferir postura a partir de si la ventana está maximizada.

### 3.4 Batería y alimentación

GetSystemPowerStatus informa alimentación CA/CC, estado de carga, autonomía estimada y ahorro de batería. Esto es estado dinámico. La presencia de batería es evidencia complementaria, no criterio aislado para clasificar el chasis. Los perfiles deben reevaluar AC/DC antes de operaciones largas y al recibir eventos de cambio de energía.

### 3.5 Touch, pen y teclado

Usar las APIs de entrada de Windows para enumerar las clases y capacidades disponibles (touch, mouse, pen/stylus, teclado, touchpad y eventos de postura cuando estén expuestos por el fabricante). Touch o pen son capacidades de interacción: no determinan por sí solas un tablet. Distinguir teclado integrado, conectado y externo siempre que el sistema exponga la diferencia; cuando no sea fiable, declarar el estado como desconocido.

### 3.6 Otras señales

- Docking: detectar estado y cambios cuando el sistema lo exponga.
- GPU: modelo, proveedor, dispositivos presentes y capacidades; diferenciar GPU integrada/dedicada sin asumir que la dedicada siempre está activa.
- Almacenamiento: SSD SATA/NVMe, HDD, volúmenes, espacio y salud disponible.
- CPU/memoria: arquitectura, topología, núcleos/hilos y memoria disponible.
- Térmica: sensores y valores solo cuando sean proporcionados por hardware/driver; no inventar temperaturas a partir de carga de CPU.
- Seguridad: estado de TPM, Secure Boot, VBS/Memory Integrity, Defender, Firewall y BitLocker, siempre lectura por defecto.
- Virtualización/sesión remota: detectar entornos virtuales y sesiones remotas para evitar recomendaciones que supongan hardware físico local.

## 4. Modelo de perfil y confianza

El perfil normalizado debe almacenar:

- form_factor: desktop | notebook | tablet | convertible | detachable | all_in_one | workstation | handheld | virtual | unknown.
- classification_confidence: high | medium | low.
- classification_evidence: fuente, dato observado, timestamp, calidad y si hubo error.
- capabilities: flags tri-state (true | false | unknown) para batería, CA/CC, touch, pen, teclado físico/acoplado, tapa, docking, sensores, GPU dedicada, SMART, temperatura y demás funciones necesarias.
- hardware_summary: fabricante/modelo/CPU/GPU/RAM/discos con minimización de identificadores.
- operating_context: energía, carga/temperatura si disponible, modo de ahorro y estado de sesión.
- platform: Windows build, edición, arquitectura y drivers relevantes.

No usar booleanos falsos para datos que no se pudieron leer. unknown no equivale a false.

### Resolución de conflictos

- Preferir señales de hardware documentadas y específicas (SMBIOS/DeviceForm) sobre una etiqueta genérica.
- Usar WMI PCSystemType/Ex, batería y entrada para corroborar.
- Si fuentes fuertes contradicen otras (por ejemplo, chasis Desktop y tipo Slate), clasificar como unknown/low y mostrar la discrepancia.
- No adivinar con heurísticas como el tamaño de pantalla o el número de baterías.
- Permitir corrección manual documentada por el usuario, preservando la detección original para diagnóstico.
- No escribir una etiqueta de equipo en el registro de Windows para forzar una clasificación.

## 5. Cómo adaptará SOPTRAZ los perfiles

### Desktop / workstation

- No ofrecer optimizaciones de autonomía como perfil automático por defecto.
- Detectar GPU, refrigeración, almacenamiento y memoria para ajustar el diagnóstico y el catálogo de gaming/creación.
- Distinguir desktop común, workstation y all-in-one.
- No cambiar ventiladores, voltajes, overclock, BCD ni mitigaciones de CPU.
- No asumir que desktop equivale a buen flujo térmico o potencia estable; leer sensores cuando existan.

### Notebook

- Evaluar batería, estado CA/CC, salud y ahorro de batería; reevaluar antes de tareas largas.
- En batería, priorizar autonomía y limitar tareas pesadas solo con consentimiento y política visible.
- En CA, ofrecer un perfil de rendimiento compatible, sin asumir que es adecuado si hay throttling/temperatura alta.
- Preservar tapa, suspensión, teclas especiales, touchpad, Wi-Fi, sensores y utilidades OEM.
- No deshabilitar funciones de energía o dispositivos que el fabricante usa para thermal/power management.

### Tablet

- Priorizar interacción táctil, rotación, sensores, teclado virtual, lápiz, suspensión y autonomía.
- No deshabilitar HID, touch, pen, sensores, rotación, cámara, conectividad celular ni servicios de energía/entrada bajo un perfil genérico.
- Ajustar la interfaz de Toolbox para touch: controles amplios, menos menús anidados y targets accesibles.
- Si no existe teclado físico, no mostrar atajos que requieran teclado como única forma de acceso.

### Convertible / detachable (2-en-1)

- Mantener form factor estable y detectar por separado el estado dinámico del teclado/postura.
- Cambiar solo la presentación de SOPTRAZ cuando se desacopla el teclado; no aplicar automáticamente tweaks de rendimiento al plegar la pantalla.
- Preservar sensores, rotación, touch, pen, teclado acoplable y estados de suspensión.
- La clasificación no debe fluctuar al conectar un teclado USB externo.

### All-in-one y handheld

- All-in-one: mantener clase propia; puede tener touch aunque el perfil general sea desktop.
- Handheld: preservar controles integrados, sensores, administración térmica y perfiles OEM. Tratarlo como una clase especializada, no como notebook común.

### Virtual / Remote

- Evitar recomendaciones sobre BIOS, firmware, GPU física, SMART de hardware host o drivers del equipo anfitrión.
- Reportar qué datos pertenecen a la VM o sesión y cuáles no están accesibles.
- No inferir la configuración del host.

## 6. Perfil adaptativo no es automatización silenciosa

Flujo requerido:

1. **Detectar:** recolectar señales read-only con origen y error.
2. **Clasificar:** form factor, capacidades y confianza.
3. **Evaluar:** determinar acciones elegibles y por qué otras se bloquean.
4. **Recomendar:** construir plan según hardware, build, energía, políticas y objetivo del usuario.
5. **Previsualizar:** explicar cada cambio, impacto, privilegio, reinicio y rollback.
6. **Aprobar:** pedir consentimiento según riesgo.
7. **Revalidar:** antes de ejecutar, volver a leer precondiciones y energía/postura para evitar estado desactualizado.
8. **Ejecutar:** solo después de M1.4/M1.5 y con supervisor.
9. **Verificar:** comprobar resultado, side effects y evidencia.
10. **Restaurar:** respetar snapshot real y no sobrescribir cambios externos.

Las recomendaciones pueden actualizarse cuando cambia la energía, se acopla/desacopla teclado o se conecta un dock. Ningún evento de estado debe ejecutar mutaciones por sí solo.

## 7. Ejemplos de decisiones adaptativas

| Condición observada | Recomendación SOPTRAZ | Acción automática prohibida |
|---|---|---|
| Notebook en batería y ahorro activo | Posponer benchmark/escaneo pesado y ofrecer modo batería | Forzar plan de máximo rendimiento |
| Notebook conectada a CA | Habilitar benchmark/diagnóstico bajo demanda | Cambiar CPU/GPU/voltajes sin autorización |
| Tablet táctil sin teclado físico | Toolbox touch-first, conservar lápiz y sensores | Deshabilitar servicios HID/sensores |
| Convertible desacoplada | Cambiar layout de SOPTRAZ y reevaluar accesos | Aplicar tweaks de registro o energía por postura |
| Desktop con GPU dedicada | Ofrecer inventario y configuración gráfica por aplicación | Desactivar GPU integrada o drivers sin evidencia |
| Workstation bajo carga/temperatura elevada | Mostrar presión térmica y diferir trabajos pesados | Desactivar límites térmicos o modificar ventiladores |
| VM / sesión remota | Mostrar inventario accesible con límites | Afirmar conocer BIOS/GPU/discos del host |
| Señales ambiguas | Reportar clasificación incierta | Aplicar un perfil por defecto |

## 8. Contrato de datos de referencia

Ejemplo conceptual, todavía no es un JSON Schema aceptado ni un contrato ejecutable:

~~~json
{
  "schema_version": "1.0",
  "form_factor": "convertible",
  "classification_confidence": "high",
  "classification_evidence": [
    {"source": "Win32_SystemEnclosure.ChassisTypes", "value": 31, "observed": true},
    {"source": "ConvertibleSlateMode", "value": 0, "observed": true}
  ],
  "capabilities": {
    "battery_present": true,
    "power_source": "ac",
    "touch": true,
    "pen": true,
    "physical_keyboard": true,
    "keyboard_attached": false,
    "dedicated_gpu": false,
    "thermal_sensors": "unknown"
  },
  "recommendation_only": true
}
~~~

En producción, el esquema debe validar enumeraciones, límites de tamaño, campos obligatorios y valores tri-state. Datos desconocidos u omitidos nunca habilitan acciones por defecto.

## 9. Privacidad y seguridad

- No almacenar seriales de BIOS, UUID de hardware, direcciones MAC ni identificadores de usuario en el perfil salvo necesidad justificada y consentimiento específico.
- Preferir fabricante/modelo genérico, capacidades y drivers estrictamente necesarios para el diagnóstico.
- No enviar inventario a un Hub o proveedor de IA por defecto; mostrar vista previa y minimizar datos.
- No ejecutar WMI/PowerShell arbitrario recibido desde UI o IA.
- Lectura de bajo privilegio por defecto; cada acceso que requiera elevación se declara y justifica.
- Las señales se tratan como datos potencialmente defectuosos: manejar ausencia, timeout, WMI corrupto, permisos y proveedor OEM.
- El perfil no constituye una prueba criptográfica del dispositivo ni una identidad segura.

## 10. Pruebas y aceptación

### Unitarias con fixtures

- Mapping de todos los códigos de chasis publicados.
- PCSystemType desconocido y no soportado.
- Tablet/convertible/detachable y conflictos de señales.
- Desktop con touch; notebook sin reporte de batería; batería reportada en equipo no portátil.
- Convertible con teclado attach/detach y teclado USB externo.
- WMI ausente/timeout/errores/valores múltiples.
- VM/Remote, all-in-one y handheld.
- unknown nunca se convierte implícitamente en false ni habilita acciones.
- Mismo input produce mismo perfil y evidencia ordenada.

### Integración Windows

Matriz de hardware real/VM cuando esté disponible: desktop tower, all-in-one, notebook, tablet sin teclado, convertible/detachable y VM/Remote; Windows 10/11 y builds dentro del soporte declarado. Registrar casos donde SMBIOS OEM está mal informado; no corregirlo con heurísticas silenciosas.

### Criterios de aceptación

- Expone form factor, capacidades, confianza y fuentes de evidencia.
- Nunca confunde touch con tablet ni batería con notebook por sí solos.
- Separa postura convertible de clase física.
- Reevalúa estado dinámico antes de cualquier acción.
- Los perfiles solo incluyen acciones con precondiciones compatibles.
- Ambigüedad produce estado unknown y recomendación conservadora.
- Solo lectura no necesita elevación innecesaria.
- No incluye acciones mutantes antes de M1.4 y M1.5.
- Todos los tests de fixtures, CI multiplataforma y pruebas Windows específicas pasan.

## 11. Fuentes oficiales

- Microsoft Learn, [Recommended settings for better tablet experiences](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/settings-for-better-tablet-experiences) — DeviceForm, Enclosure Type, ConvertibleSlateMode y ConvertibilityEnabled.
- Microsoft Learn, [Win32_SystemEnclosure](https://learn.microsoft.com/en-us/windows/win32/cimwin32prov/win32-systemenclosure) — ChassisTypes y códigos SMBIOS.
- Microsoft Learn, [Win32_ComputerSystem](https://learn.microsoft.com/en-us/windows/win32/cimwin32prov/win32-computersystem) — PCSystemType/PCSystemTypeEx.
- Microsoft Learn, [GetSystemPowerStatus](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getsystempowerstatus) — alimentación CA/CC y estado de batería.
- Microsoft Learn, [ConvertibleSlateMode](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-gpiobuttons-convertibleslatemode) — presencia/accesibilidad de teclado físico.
- Microsoft Learn, [Input devices](https://learn.microsoft.com/en-us/windows/apps/develop/input/) y [Handle pointer input](https://learn.microsoft.com/en-us/windows/apps/develop/input/handle-pointer-input) — touch, pen/stylus y otros dispositivos de entrada.

La investigación distingue el contrato y las fuentes oficiales de la implementación futura. El inventario de hardware actual de SOPTRAZ no debe considerarse existente hasta que se entregue y pruebe el módulo read-only.


## 12. Estado de implementación (2026-11-28)

### Integrado en main

- Clasificador puro `internal/modules/deviceprofile`: form factor, confianza, evidencia, capacidades tri-state y manejo conservador de conflictos.
- Recolector de plataforma `internal/platform/deviceprofile`: extracción acotada del tipo de chasis SMBIOS Type 3; `GetSystemPowerStatus`; `GetPointerDevices`; `GetSystemMetrics` para slate/dock cuando corresponde.
- El parser SMBIOS valida encabezado, longitud, estructuras, terminadores, límite de tamaño y marcador End-of-Table obligatorio.
- La recolección tolera fallos parciales, mantiene `unknown` y no recoge seriales, UUID, MAC ni asset tags.
- No-Windows devuelve error de capacidad no disponible y mantiene el mismo contrato de datos.

### En revisión en PR #18

- Comando `soptraz device-profile [--json]` para presentar el resultado como informe humano o JSON.
- Estados tri-state explícitos como cadenas `yes | no | unknown`.
- Reporte de errores por fuente y marca `mutations_applied: false`.

### Lo que todavía falta

- Recolección confiable de DeviceForm/PCSystemType, teclado físico, GPU dedicada, sensores térmicos, detalles de almacenamiento y contexto de virtualización/Remote.
- Integración de este informe con la UI Wails/Field y Toolbox.
- Recomendaciones de perfiles adaptativos; no se evalúan ni ejecutan acciones mutantes todavía.
- Pruebas en hardware físico representativo: desktop, notebook, tablet y convertible. La CI de Windows verifica compilación y ABI con el runner, pero no sustituye la prueba en cada OEM/chasis real.

El inventario actual es una primera base verificable, no un detector completo de capacidades ni un benchmark del equipo.
