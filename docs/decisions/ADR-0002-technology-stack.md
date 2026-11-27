# ADR-0002: Go, Wails v3 y TypeScript/React

- Estado: Aceptada para prototipo
- Fecha: 2026-11-27

## Contexto

Field necesita escritorio multiplataforma, consumo moderado, operación local y acceso a APIs de sistema. Hub necesita UI web y servicios reutilizables.

## Decisión

- Go para runtime, CLI, API, orquestación y adaptadores.
- Wails v3 para contenedor desktop.
- TypeScript + React + Vite para UI compartida.
- UI desacoplada del dominio por contratos.
- Hub sirve la misma UI web.
- Linux documenta requisitos GTK/WebKit y ofrece modo web local en hosts incompatibles.

## Alternativas

- Electron: ecosistema sólido, pero navegador/runtime más pesado.
- Tauri: buena alternativa, requiere host Rust adicional.
- Web exclusiva: simple, pero menor integración desktop.
- Wails v2: madura; v3 declara estable soporte desktop y modelo actualizado.

## Consecuencias

Reducir duplicación entre Field y Hub. Probar Wails en matriz real antes de declarar compatibilidad. No asumir que todos los Linux tienen WebKit/GTK compatible.
