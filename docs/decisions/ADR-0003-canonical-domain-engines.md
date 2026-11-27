# ADR-0003: Un núcleo canónico por dominio

- Estado: Aceptada
- Fecha: 2026-11-27

## Contexto

SOPTRAZ usa Go, Python, TypeScript y scripts nativos. Sin reglas, la lógica se duplicará y divergirá.

## Decisión

Cada responsabilidad tiene propietario:
- Go: estado y reglas operativas, orquestación, autorizaciones, auditoría y dispatch.
- Python: algoritmos numéricos/estadísticos, series temporales, análisis, modelos y scoring cuantitativo.
- TypeScript: presentación y estado UI, sin cálculos autoritativos.
- PowerShell/Bash/zsh y APIs nativas: adaptadores OS finos, sin lógica de negocio duplicada.

Algoritmos Python expuestos mediante JSON Schema versionado y worker supervisado. Fixtures compartidos. No se copia el algoritmo a Go o frontend.

Aritmética trivial de infraestructura no justifica un proceso Python. La propiedad canónica aplica a algoritmos de dominio/analítica.

## Alternativas descartadas

- Un lenguaje para todo: ignora fortalezas especializadas.
- Un lenguaje por módulo pequeño: dispersión y contratos innecesarios.
- Duplicar lógica por UI: divergencia.

## Consecuencias

Python se incorpora con una capacidad analítica concreta. Field empaqueta el worker por plataforma cuando se necesite, sin exigir Python instalado al usuario.
