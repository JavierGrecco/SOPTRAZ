# POC de proveedores de tiempo confiable: evaluación y puertas de aceptación

- Fecha de revisión: 2026-11-28.
- Estado: POC aislada de Roughtime RFC 10049 v1 implementada en la rama draft del PR #31; NTS sigue en evaluación. La dependencia está fijada en esa rama, pero el adaptador no está conectado a la CLI, al binario de producción, al cliente TUF ni a la persistencia anti-rollback.
- Relacionado: [ADR-0020](../decisions/ADR-0020-trusted-time-and-clock-recovery.md), [ADR-0021](../decisions/ADR-0021-os-time-diagnostics.md) y [contrato TrustedTimeProvider](../architecture/trusted-time-provider.md).
- Objetivo: resolver el bootstrap cuando el reloj del equipo es incorrecto, sin desactivar la validación TLS, ampliar la vigencia de metadata TUF ni cambiar el reloj del sistema automáticamente.

## Resumen ejecutivo

La evaluación mantiene dos funciones separadas:

1. **Roughtime RFC 10049** es candidato para obtener una primera referencia aproximada aun cuando el cliente no conoce la hora. La clave pública del servidor debe estar provisionada fuera de la respuesta. El protocolo aporta una respuesta firmada vinculada a un nonce fresco y un intervalo de incertidumbre; la seguridad real exige comparar operadores independientes y mantener una lista de claves gobernada por SOPTRAZ.
2. **NTS (RFC 8915)** es candidato para consultas autenticadas posteriores, una vez que el reloj local permite validar normalmente los certificados TLS de NTS-KE. No resuelve por sí solo un primer contacto con un reloj gravemente incorrecto.

La evaluación produjo una POC aislada de Roughtime, no una selección de producción ni de servidores públicos. El adaptador usa servidores TCP/UDP de laboratorio y claves generadas por los tests; todavía faltan vectores oficiales e interoperabilidad con una implementación independiente. NTS permanece como candidato posterior. La POC no modifica el updater go-tuf, no añade excepciones de certificados y no habilita catálogo offline.

## Hallazgos de protocolo

### Roughtime — RFC 10049

RFC 10049 fue publicado en octubre de 2026 como **Experimental**, no como Internet Standard. Su objetivo es permitir sincronización temporal aproximada incluso cuando el cliente no tiene una idea inicial de la hora, y producir evidencia de inconsistencias entre servidores. La respuesta incluye una firma vinculada al contenido de la solicitud y su nonce, además de un intervalo de incertidumbre. La firma demuestra qué afirmó el servidor después de recibir la solicitud; no demuestra por sí sola que el servidor esté bien sincronizado ni que diga la verdad.

El propio RFC deja fuera de alcance la gobernanza del ecosistema: distribución/mantenimiento de listas de servidores confiables y políticas para procesar informes de mal comportamiento. Eso debe resolverlo SOPTRAZ mediante una lista de claves y operadores provisionada de forma independiente, versionada y autenticada. Nunca se debe aprender una raíz pública de la misma respuesta cuya hora se pretende verificar.

Referencias:
- [RFC 10049 — Roughtime: A Protocol for Rough Time Synchronization](https://www.rfc-editor.org/rfc/rfc10049.html).
- [Datatracker — estado Experimental y fecha de publicación](https://datatracker.ietf.org/doc/html/rfc10049).

### Implementación Go evaluada: tannerryan/roughtime

La rama `fix/tuf-crash-stage-recovery` del PR #31 contiene una POC del paquete `internal/platform/trustedtime` basada en [github.com/tannerryan/roughtime](https://github.com/tannerryan/roughtime):

- Dependencia fijada: `github.com/tannerryan/roughtime v1.28.4`, tag resuelto al commit upstream `7454b45d337b413d3639da88acf5559d08d7af27`; licencia BSD-2-Clause; Go 1.27.4.
- El adaptador usa exclusivamente el paquete `protocol`, RFC 10049 v1 strict y raíces Ed25519 provisionadas localmente. No activa `Flex`, Google-Roughtime ni extensiones experimentales ML-DSA.
- La configuración exige entre tres y ocho testigos, operadores distintos, claves raíz distintas, `TrustAnchorID` distintos y huellas SHA-256 coherentes con `Source.KeyID`. La identidad y la independencia real de cada operador siguen siendo responsabilidad de la provisión/gobernanza, no las demuestra el protocolo.
- Cada medición realiza dos pasadas secuenciales en el mismo orden aleatorio, con nonces frescos encadenados a la respuesta autenticada anterior. Cada respuesta se verifica antes de entrar en la cadena y la cadena completa se valida de nuevo para comprobar firmas, nonces y orden causal.
- Se admiten endpoints UDP y TCP, con timeouts de intercambio y de medición, límites explícitos de incertidumbre y rechazo total del lote ante respuesta incompleta o inválida. No se devuelve evidencia parcial.
- Los intervalos de la segunda pasada se proyectan al final de la medición usando tiempos transcurridos monotónicos y RTT; la hora de pared local no se usa como referencia confiable.
- Los tests levantan servidores locales UDP y TCP y cubren dos pasadas, nonces únicos, firma incorrecta, replay, delegación firmada fuera de su ventana MINT/MAXT, tiempos causalmente decrecientes, radio excesivo, configuración duplicada y cancelación.

La biblioteca también incluye servidor y herramientas CLI, pero SOPTRAZ no incorpora esos binarios. La dependencia figura en el `go.mod` de esta rama experimental; el proveedor no está conectado a la CLI, al binario de la aplicación, al `TUFClient` ni a la política persistente. La POC no constituye una selección de producción. CI #339 pasó 6/6 en Windows, macOS, Ubuntu, Race Detector, fuzzing e higiene de ramas.

**Límites que no deben confundirse con una validación completa:**

- Verificar `MIDP` dentro del intervalo firmado `MINT..MAXT` demuestra coherencia con la delegación, pero no compara esa delegación con un reloj local ni demuestra que la primera respuesta sea reciente. Varios operadores que acuerden una hora antigua pueden presentar una cadena causalmente coherente.
- Claves distintas, OperatorID distintos y TrustAnchorID distintos son requisitos de configuración, no prueba independiente de que las organizaciones, infraestructuras o fuentes ascendentes no estén correlacionadas.
- Los tests usan la misma biblioteca para generar y verificar las respuestas de laboratorio. Aún faltan vectores oficiales RFC 10049, interoperabilidad con otra implementación, revisión independiente de la dependencia y gobernanza operativa de provisión, rotación y revocación de raíces.
- No existe persistencia de `LastAcceptedTrustedTime`, ni recuperación bootstrap con frescura demostrable ante un reloj inicial gravemente incorrecto, ni integración TUF. La POC no cambia el reloj del SO, no desactiva TLS, no extiende expiraciones y no habilita offline.


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

## Primera política de aceptación incorporada (2026-11-28)

La POC ya cuenta con una base de política pura en `internal/platform/trustedtime/policy.go`, sin incorporar aún adaptadores externos:

- `AcceptancePolicy` exige una allowlist de `Source` provisionada localmente y compara todos los campos de identidad: ID, operador, tipo, assurance, ancla y KeyID.
- Solo admite clases `authenticated` y `attested`; la hora del sistema queda fuera de la confianza criptográfica.
- No cuenta dos respuestas del mismo `OperatorID` como dos operadores.
- Aplica límites configurables de incertidumbre por fuente y sobre el intervalo combinado.
- Selecciona la intersección máxima única de intervalos; rechaza evidencia insuficiente o dos grupos máximos empatados.
- Conserva cada `Evidence` canónica seleccionada y su digest, además del intervalo agregado.
- Aplica el límite inferior del último intervalo aceptado como piso anti-rollback, sin extender el extremo superior.

La cobertura actual contiene 18 tests específicos y CI #329 pasó 6/6 en Windows, macOS, Ubuntu, Race Detector, fuzzing y branch hygiene ([run #329](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46161082788)).

**Límites expresos:** esta política no verifica firmas, TLS, claves, nonce ni frescura; esas garantías pertenecen a los futuros adaptadores. Tampoco demuestra independencia administrativa real, no establece por sí misma un quorum de producción y aún no persiste el piso. No lee `time.Now()`, no cambia el reloj y no habilita TUF offline.

## Plan de POC y criterios de aceptación

### POC Roughtime aislada

- [x] Fijar el commit upstream `7454b45d337b413d3639da88acf5559d08d7af27` y registrar tag, licencia y versión Go en la POC; queda pendiente una revisión de seguridad independiente de todo el código importado, transitivas y advisories.
- [x] Probar al menos tres servidores locales con raíces, operadores y TrustAnchorID distintos; la configuración rechaza IDs, operadores, raíces y anclas duplicados. La demostración de independencia organizativa real y de DNS/infraestructura compartidos sigue pendiente.
- [x] Implementar y probar RFC 10049 v1 strict sobre UDP y TCP, dos pasadas causales, timeouts y cancelación con servidores locales construidos con la biblioteca fijada. [ ] Probar vectores oficiales RFC e interoperabilidad con una implementación independiente.
- [x] Cubrir firma incorrecta, replay/nonce repetido, delegación cuyo MIDP queda fuera de MINT/MAXT, radio excesivo y configuración de claves/anclas duplicadas. [ ] Añadir casos dirigidos de clave desconocida, datagramas/respuestas truncadas y límites máximos de tamaño.
- [x] Rechazar un lote con tiempos causalmente decrecientes y no devolver evidencia parcial. [ ] Profundizar escenarios de colusión, operador comprometido, mayoría insuficiente y contradicción con respuestas de otra implementación.
- [x] Ejecutar Race Detector y CI Windows/macOS/Ubuntu (CI #339, 6/6). [ ] Añadir fuzzing específico del parser/proveedor Roughtime; el job fuzz actual del repositorio no equivale a fuzzear el protocolo.
- [x] La API del proveedor no expone operaciones para cambiar reloj o configuración local y no persiste evidencia; el flujo sigue aislado y sin wiring productivo. [ ] Añadir prueba de no-mutación del estado del sistema y de ausencia de escrituras en integración.

### POC NTS posterior al bootstrap

- [ ] Fijar commit/version, revisar `beevik/nts`, `beevik/ntp`, SIV/AEAD y transitivas.
- [ ] Probar con Chrony o NTPsec de laboratorio; cubrir NTS-KE TLS, certificados no válidos, cookies, unique ID, autenticación AEAD y respuesta manipulada.
- [ ] Confirmar que un reloj incorrecto no permite saltar TLS; Roughtime debe establecer primero un intervalo bootstrap aceptable.
- [ ] Probar pérdida de cookies, rotación de certificado/clave, servidores no conformes, reintentos y timeouts.
- [ ] No considerar una respuesta NTS de un único operador como independencia suficiente ante compromiso de ese operador.

### Integración de política, separada de adaptadores

- [x] Implementar política pura en memoria para allowlist exacta, operadores únicos por lote, intersección común máxima, incertidumbre y piso anti-rollback; conserva las evidencias seleccionadas y no realiza I/O.
- [ ] Definir los valores operativos de producción para mínimo de operadores, límites de incertidumbre/frescura y gestión de contradicciones a partir del modelo de amenazas y pruebas de laboratorio.
- [ ] Persistir `LastAcceptedTrustedTime` con procedencia y evidencia verificable mediante migración SQLite v2→v3; timestamps antiguos de pared permanecen `system/local-untrusted`.
- [ ] Garantizar que rollback o fallo de commit deja intacto el estado previamente aceptado.
- [ ] Integrar con TUF sin `UnsafeSetRefTime`, sin saltarse validación X.509 y sin prolongar expiración.
- [ ] Acreditar con pruebas que ningún camino de error devuelve `VerifiedTarget` cuando la hora o la evidencia temporal no son suficientes.

## Decisión provisional

**No añadir aún Roughtime ni NTS al flujo de producción.** La POC Roughtime RFC 10049 v1 está implementada de forma aislada en el PR #31, con la dependencia fijada en esa rama. Sigue pendiente validar bootstrap/frescura, independencia operativa, vectores oficiales, interoperabilidad, rotación/revocación, persistencia anti-rollback y conexión con TUF. NTS queda como candidato posterior a un bootstrap temporal aceptable. Esta POC no cierra M1.4, no habilita catálogo offline y no modifica la política de expiración.
