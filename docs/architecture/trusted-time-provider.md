# Contrato de evidencia de tiempo confiable

Estado: contrato puro implementado; proveedores e integración TUF pendientes.  
Relacionado: [ADR-0020](../decisions/ADR-0020-trusted-time-and-clock-recovery.md).

## Alcance de esta primera entrega

El paquete internal/platform/trustedtime establece un límite pequeño y sin efectos secundarios:

- Interval representa un intervalo UTC [Earliest, Latest], no una hora puntual con precisión ficticia.
- Source identifica de forma estable el proveedor, su operador administrativo, el mecanismo y la clase de garantía declarada.
- Evidence agrupa el intervalo y su procedencia.
- TrustedTimeProvider define Observe(context.Context), sin acoplar consumidores a NTS, Roughtime ni APIs específicas del sistema operativo.
- Validate comprueba únicamente invariantes estructurales.
- Canonical devuelve una copia en UTC y elimina lecturas monotónicas locales antes de serializar o persistir.

El paquete no incluye red, criptografía, almacenamiento, lectura del reloj, modificación del reloj del sistema ni selección automática de fuentes. No se agrega ninguna dependencia al binario.

## Semántica de la evidencia

Earliest y Latest son los límites del intervalo informado por el proveedor. La anchura se obtiene mediante Interval.Uncertainty().

Clases de garantía:

- system: estado o señal de sincronización del servicio horario del sistema operativo; no equivale a una atestación criptográfica.
- authenticated: intercambio autenticado por el protocolo, por ejemplo una asociación NTS válida.
- attested: afirmación temporal firmada y verificada con una clave anclada por configuración independiente.

Tipos de fuente admitidos: system, nts, roughtime y custom. Estos valores describen el mecanismo; no determinan por sí solos que una fuente sea aceptable.

Source.ID es un identificador local estable, no un valor copiado de la respuesta remota. OperatorID identifica quién administra realmente la fuente. Si está vacío, la independencia del operador es desconocida: dos hosts o nombres DNS diferentes no prueban que existan operadores independientes. TrustAnchorID identifica el ancla configurada fuera de la respuesta de tiempo y es obligatorio para las clases authenticated y attested.

## Obligaciones del consumidor

Una evidencia que pasa Validate sigue siendo una afirmación, no una autorización. Antes de utilizarla para decisiones de seguridad, el consumidor debe:

1. aplicar una allowlist de IDs, mecanismos y anclas provisionadas de forma independiente;
2. verificar la autenticidad protocolar y la prueba criptográfica en el adaptador, antes de devolver evidencia autenticada o atestada;
3. aplicar política de frescura, incertidumbre máxima y diversidad real de operadores;
4. contrastar intervalos con LastAcceptedTrustedTime y rechazar retrocesos/replays sin convertir time.Now() en un ancla confiable;
5. persistir la evidencia aceptada con su procedencia mediante una migración atómica;
6. para TUF, considerar vencida la metadata cuando expires <= Interval.Latest; nunca usar la incertidumbre para extender su vigencia.

Los errores del proveedor no deben filtrar respuestas crudas, secretos, tokens ni material criptográfico. Toda operación de red futura deberá respetar el context, timeout y límites de tamaño. Ningún proveedor cambiará el reloj del sistema automáticamente.

## Lo que todavía no está implementado

- Proveedores nativos de solo lectura para Windows, macOS y Linux, y un diagnóstico separado de SystemWallClock.
- Pruebas de NTS y Roughtime en laboratorio, incluidas claves/raíces, nonce, replay, incertidumbre, fuentes contradictorias y rotación.
- Política de selección y combinación de varias fuentes independientes.
- Persistencia SQLite v3 de LastAcceptedTrustedTime y migración v2 → v3 sin convertir fechas locales antiguas en evidencia.
- Recuperación temporal online y conexión al flujo de actualización.
- Referencia temporal compatible con go-tuf. El Updater oficial actual no recibe este contrato; esta entrega no altera su reloj de referencia ni introduce UnsafeSetRefTime.

Por tanto, la creación del paquete no habilita el catálogo offline, no devuelve VerifiedTarget y no cambia el estado de aceptación de M1.4.
