# AI Gateway — política de IA

**Política aprobada:** proveedores gratuitos primero, alternancia automática y modelo local habilitado. Gasto automático prohibido.

## Objetivos

- Proveedores intercambiables: Gemini, OpenRouter, Groq, Cloudflare, OpenAI, DeepSeek u otros adaptadores validados.
- Nivel gratuito cuando esté disponible y sus condiciones lo permitan.
- Failover por cuota, rate limit, indisponibilidad o error transitorio.
- Ollama/modelo local cuando esté instalado.
- Operaciones esenciales sin dependencia de IA.
- Ningún gasto API por defecto.

## Política de costos

- free_only predeterminado.
- Solo utilizar modelos cuyo costo cero esté confirmado para la cuenta y el momento de consulta.
- Si precio/elegibilidad no puede verificarse, deshabilitar en free-only.
- Nunca fallback a modelos pagos.
- No rotar cuentas para eludir límites o condiciones.
- Cada organización utiliza sus propias credenciales y cuotas.
- Modo pago futuro solo mediante habilitación explícita y presupuesto.

## Selección

Evaluar capacidades, privacidad, costo, cuota observable, disponibilidad, latencia, contexto y salida estructurada. Los modelos no son equivalentes: descartar los que no cumplen el contrato.

## Failover

- Respetar Retry-After/cabeceras.
- Backoff y circuit breaker.
- No reintentar infinitamente.
- No repetir operaciones no idempotentes.
- No mandar automáticamente la misma evidencia a múltiples proveedores.
- Registrar modelo, uso/costo cuando sea posible, motivo del fallback y resultado.
- Si todos fallan: modelo local o procedimientos determinísticos.

## Privacidad

- No enviar evidencia sin consentimiento y política organizacional.
- Mostrar vista previa del paquete.
- Excluir/pseudonimizar hostname, seriales, usuarios, rutas, direcciones y nombres de cliente por defecto.
- Nunca enviar contraseñas, tokens, claves BitLocker/FileVault/API, volcados de memoria ni archivos personales.
- No persistir claves API en USB.
- Preferir almacenes seguros del SO o credenciales efímeras.
- Registrar retención y uso de datos por proveedor.
- Un proveedor gratuito cuyos términos permitan usar entradas para mejorar modelos no recibe datos sensibles sin autorización y anonimización apropiada.

## Límite de autoridad

La IA analiza evidencia saneada, formula hipótesis, cita evidencias y propone procedimientos registrados. No ejecuta shell libre, inventa procedimientos, eleva privilegios, aprueba su recomendación ni altera auditoría.

La respuesta se valida con JSON Schema y reglas de negocio. Validez estructural no implica recomendación segura.

## Modo local

Ollama es opcional. No descargar modelos grandes automáticamente. Informar tamaño, requisitos de RAM/VRAM, licencia y capacidades. Sin modelo compatible se continúa con reglas determinísticas.

## Proveedores

Catálogo dinámico con fecha de verificación. Cuotas, modelos, precios y condiciones cambian; no son garantías permanentes.
