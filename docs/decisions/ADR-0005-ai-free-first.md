# ADR-0005: AI Gateway Free-first, failover y modelo local

- Estado: Aceptada
- Fecha: 2026-11-27

## Decisión

Política free_only.
- Alternancia automática entre proveedores/modelos gratuitos compatibles.
- Ollama/local como alternativa cuando esté instalado.
- Sin gastos automáticos.
- Sin rotación de cuentas ni evasión de cuotas.
- No enviar datos sensibles sin consentimiento y saneamiento.
- No depender de IA para diagnóstico/reparación esencial.

## Proveedores candidatos

Gemini, OpenRouter, Groq, Cloudflare AI y otros con condiciones gratuitas verificadas; OpenAI y DeepSeek como conectores opcionales deshabilitados bajo free-only salvo cuota gratuita explícita y verificable.

## Consecuencias

Cuotas, modelos y precios cambian. Catálogo dinámico con fecha de verificación. Si no se puede confirmar costo cero, se deshabilita en free-only. Fallback local o reglas determinísticas.
