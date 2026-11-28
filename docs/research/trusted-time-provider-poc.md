# POC de proveedores de tiempo confiable: evaluación y puertas de aceptación

- Fecha de revisión: 2026-11-28.
- Estado: investigación técnica; no se incorpora ninguna dependencia de producción.
- Relacionado: [ADR-0020](../decisions/ADR-0020-trusted-time-and-clock-recovery.md), [ADR-0021](../decisions/ADR-0021-os-time-diagnostics.md) y [contrato TrustedTimeProvider](../architecture/trusted-time-provider.md).
- Objetivo: resolver el bootstrap cuando el reloj del equipo es incorrecto, sin desactivar la validación TLS, ampliar la vigencia de metadata TUF ni cambiar el reloj del sistema automáticamente.

## Resumen ejecutivo

La evaluación mantiene dos funciones separadas:

1. **Roughtime RFC 10049** es candidato para obtener una primera referencia aproximada aun cuando el cliente no conoce la hora. La clave pública del servidor debe estar provisionada fuera de la respuesta. El protocolo aporta una respuesta firmada vinculada a un nonce fresco y un intervalo de incertidumbre; la seguridad real exige comparar operadores independientes y mantener una lista de claves gobernada por SOPTRAZ.
2. **NTS (RFC 8915)** es candidato para consultas autenticadas posteriores, una vez que el reloj local permite validar normalmente los certificados TLS de NTS-KE. No resuelve por sí solo un primer contacto con un reloj gravemente incorrecto.

No se selecciona todavía ninguna biblioteca ni servidor público. La primera etapa debe ser una POC aislada, con servidores de laboratorio controlados, claves fijadas y pruebas de interoperabilidad. No se debe modificar el updater go-tuf ni añadir excepciones de certificados.

## Hallazgos de protocolo

### Roughtime — RFC 10049

RFC 10049 fue publicado en octubre de 2026 como **Experimental**, no como Internet Standard. Su objetivo es permitir sincronización temporal aproximada incluso cuando el cliente no tiene una idea inicial de la hora, y producir evidencia de inconsistencias entre servidores. La respuesta incluye una firma vinculada al contenido de la solicitud y su nonce, además de un intervalo de incertidumbre. La firma demuestra qué afirmó el servidor después de recibir la solicitud; no demuestra por sí sola que el servidor esté bien sincronizado ni que diga la verdad.

El propio RFC deja fuera de alcance la gobernanza del ecosistema: distribución/mantenimiento de listas de servidores confiables y políticas para procesar informes de mal comportamiento. Eso debe resolverlo SOPTRAZ mediante una lista de claves y operadores provisionada de forma independiente, versionada y autenticada. Nunca se debe aprender una raíz pública de la misma respuesta cuya hora se pretende verificar.

Referencias:
- [RFC 10049 — Roughtime: A Protocol for Rough Time Synchronization](https://www.rfc-editor.org/rfc/rfc10049.html).
- [Datatracker — estado Experimental y fecha de publicación](https://datatracker.ietf.org/doc/html/rfc10049).

### Implementación Go candidata: tannerryan/roughtime

La revisión del branch `main` de [github.com/tannerryan/roughtime](https://github.com/tannerryan/roughtime) observó lo siguiente:

- El `go.mod` declara Go 1.27.4, coincidente con la versión fijada por SOPTRAZ en esta revisión.
- La API strict usa RFC 10049 v1 por defecto y ofrece paquetes de cliente y protocolo. El repositorio incluye además servidor y herramientas CLI; SOPTRAZ solo evaluaría los paquetes necesarios, no incorporaría binarios auxiliares.
- La medición estricta de lista de servidores requiere al menos tres raíces distintas y tres grupos endpoint/dominio, consultados dos veces en el mismo orden aleatorio mediante una cadena causal. El agrupamiento por dominio es una heurística: nombres DNS diferentes no prueban operadores independientes.
- El README pide elegir operadores realmente independientes. La POC debe verificar y documentar el operador administrativo de cada clave; no debe inferir independencia desde IP, DNS, ASN o proveedor de hosting solamente.
- Las dependencias directas declaradas en `go.mod` son `go.uber.org/goleak`, `go.uber.org/zap`, `golang.org/x/net` y `golang.org/x/sys`; `go.uber.org/multierr` aparece como indirecta.
- La licencia declarada es BSD-2-Clause.

La cantidad mínima y el flujo de dos pasadas son requisitos del modo estricto de esa implementación y un buen punto de partida para la POC; no equivalen a un quorum criptográfico universal ni sustituyen el modelo de amenazas. Se deberá revisar el código exacto fijado por commit, el manejo de claves, nonce, respuestas parciales, timeout, replay y reportes de contradicción antes de adoptarlo.

### NTS — RFC 8915 e implementación Go candidata

[NTS (RFC 8915)](https://www.rfc-editor.org/rfc/rfc8915.html) autentica la asociación de tiempo de red mediante NTS-KE sobre TLS y protege las consultas NTP posteriores. La validación TLS ordinaria depende de la validez temporal del certificado. Por eso NTS no resuelve de manera independiente el primer contacto cuando el reloj está muy adelantado o atrasado; no se aceptará `InsecureSkipVerify`, tolerancias de certificado vencido ni una excepción temporal genérica.

La revisión de [github.com/beevik/nts](https://github.com/beevik/nts) observó:

- El `go.mod` actual declara Go 1.26.0.
- Las notas de versión consultadas incluyen v0.3.3 y correcciones anteriores relacionadas con campos únicos/autenticados de NTP y manejo de campos de extensión.
- La dependencia directa incluye `github.com/beevik/ntp v1.6.0` y `github.com/secure-io/siv-go` en una revisión pseudo-versionada de 2018; también aparecen `x/net` y `x/sys`.
- El paquete declara BSD-2-Clause.
- La documentación advierte que hay una cantidad limitada de servidores públicos NTS y que NTP Pool no ofrece NTS.

Es un candidato de laboratorio, no una dependencia aprobada. Antes de incorporarlo se deben revisar el protocolo NTS-KE, cookies, AEAD, identificadores únicos, autenticación de respuesta, validaciones de plausibilidad, mantenimiento y transitivas. Un NTS server autenticado sigue siendo un operador que puede estar comprometido o mal configurado.

Referencias:
- [RFC 8915 — Network Time Security for NTP](https://www.rfc-editor.org/rfc/rfc8915.html).
- [beevik/nts — código, documentación y notas de versión](https://github.com/beevik/nts).

## Secuencia de recuperación propuesta

1. Consultar el diagnóstico read-only del sistema operativo. La clase resultante continúa siendo `system`; el estado del servicio o del kernel no se presenta como evidencia criptográfica.
2. Si el reloj es inadecuado y existe conectividad, consultar únicamente servidores Roughtime cuya clave pública y OperatorID ya estén fijados localmente.
3. Verificar cada respuesta localmente: versión RFC, firma, nonce fresco, delegación/certificado, límites de validez, intervalo de incertidumbre, tamaño máximo, timeout y ausencia de replay.
4. Exigir diversidad administrativa real y un conjunto suficiente de intervalos compatibles. Si hay contradicciones, respuestas parciales insuficientes o incertidumbre excesiva, rechazar el conjunto completo y conservar intacto el último tiempo aceptado.
5. Usar el intervalo bootstrap para permitir una validación normal de certificados NTS-KE. NTS pasa a aportar muestras autenticadas posteriores; cualquier fallo TLS conserva la validación X.509 estricta.
6. El componente temporal entrega `Evidence` y procedencia al futuro servicio de política. No cambia el reloj del SO ni decide por sí solo sobre un target.
7. La política de SOPTRAZ acepta y persiste una evidencia solo después de comprobar allowlist, ancla, operador, frescura, dispersión, incertidumbre y no-regresión frente a `LastAcceptedTrustedTime`. El commit deberá ser atómico y no usar timestamps locales históricos como anclas.
8. La capa TUF obtiene una única referencia temporal conservadora por ciclo. Para metadata, `expires <= Interval.Latest` implica rechazo. Si no hay tiempo suficiente, no hay `VerifiedTarget`; el fallback online empieza una sesión nueva y no mezcla metadata o resultados de un intento offline.

## Límites de confianza

- Tres raíces distintas no garantizan tres operadores independientes. SOPTRAZ debe mantener OperatorID y revisar organización, infraestructura y fuentes ascendentes.
- Una firma Roughtime auténtica no prueba que la hora afirmada sea correcta. Una mayoría comprometida o correlacionada puede falsear el resultado.
- El radio/incertidumbre reportado por el servidor debe validarse contra límites de política; nunca se convierte en margen para extender metadata TUF.
- Una lista de claves vieja puede conservar confianza en un servidor comprometido. La rotación/revocación debe viajar por un canal autenticado independiente y ser auditable.
- Sin fuente de tiempo confiable o continuidad monotónica durable tras reiniciar, offline estricto puede no estar disponible. No se promete verificación offline indefinida.
- Ninguna de estas POC debe cambiar el reloj, zona horaria, servicios, configuración TLS ni estado persistido de producción.

## Plan de POC y criterios de aceptación

### POC Roughtime aislada

- [ ] Fijar un commit upstream y revisar todo el código importado, licencia, dependencias directas/transitivas y advisories alcanzables.
- [ ] Ejecutar al menos tres servidores de laboratorio con claves y operadores claramente identificados; incluir un escenario donde dos DNS apunten al mismo operador para comprobar que no se cuentan como independientes.
- [ ] Verificar RFC 10049 v1 con vectores conocidos e interoperabilidad cliente/servidor; probar UDP, fallback TCP, timeouts y cancelación.
- [ ] Rechazar firma incorrecta, clave desconocida, nonce repetido, respuesta de versión inesperada, delegación fuera de vigencia, radio excesivo, respuesta sobredimensionada y datos truncados.
- [ ] Probar contradicción entre servidores, respuesta de un operador comprometido, mayoría insuficiente y orden causal incompleto.
- [ ] Fuzzear parsers y verificadores; ejecutar Race Detector y CI Windows/macOS/Ubuntu.
- [ ] Demostrar que no se realizan escrituras al reloj ni configuración local, y que no se persiste evidencia rechazada.

### POC NTS posterior al bootstrap

- [ ] Fijar commit/version, revisar `beevik/nts`, `beevik/ntp`, SIV/AEAD y transitivas.
- [ ] Probar con Chrony o NTPsec de laboratorio; cubrir NTS-KE TLS, certificados no válidos, cookies, unique ID, autenticación AEAD y respuesta manipulada.
- [ ] Confirmar que un reloj incorrecto no permite saltar TLS; Roughtime debe establecer primero un intervalo bootstrap aceptable.
- [ ] Probar pérdida de cookies, rotación de certificado/clave, servidores no conformes, reintentos y timeouts.
- [ ] No considerar una respuesta NTS de un único operador como independencia suficiente ante compromiso de ese operador.

### Integración de política, separada de adaptadores

- [ ] Definir una política de combinación de intervalos basada en OperatorID y anclas provisionadas; especificar quorum, incertidumbre máxima, frescura y tratamiento de contradicciones antes de codificar valores de producción.
- [ ] Persistir `LastAcceptedTrustedTime` con procedencia y evidencia verificable mediante migración SQLite v2→v3; timestamps antiguos de pared permanecen `system/local-untrusted`.
- [ ] Garantizar que rollback o fallo de commit deja intacto el estado previamente aceptado.
- [ ] Integrar con TUF sin `UnsafeSetRefTime`, sin saltarse validación X.509 y sin prolongar expiración.
- [ ] Acreditar con pruebas que ningún camino de error devuelve `VerifiedTarget` cuando la hora o la evidencia temporal no son suficientes.

## Decisión provisional

**No añadir aún Roughtime ni NTS al binario principal.** La primera POC debe evaluar Roughtime RFC 10049 v1 como bootstrap con claves provisionadas fuera de banda. NTS queda como candidato posterior para observación autenticada continua después de disponer de hora suficiente para TLS. Esta recomendación es una hipótesis de laboratorio y no cierra M1.4, no habilita catálogo offline y no modifica la política de expiración.
