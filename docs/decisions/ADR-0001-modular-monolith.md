# ADR-0001: Monolito modular inicial

- Estado: Aceptada
- Fecha: 2026-11-27

## Contexto

Field necesita operación local portátil; Hub añade organización, IA e integraciones. Microservicios desde el primer día aumentarían despliegue, red, observabilidad y fallos distribuidos sin necesidad demostrada.

## Decisión

Comenzar con monolito modular Go. Los módulos tienen interfaces, contratos y datos delimitados. Procesos auxiliares se aíslan cuando el lenguaje, seguridad o ciclo de vida lo justifiquen.

## Consecuencias

- Menor complejidad de desarrollo y despliegue.
- Un ejecutable base para Field.
- Pruebas de módulo/contrato desde el inicio.
- Límites que permiten separar servicios si hay necesidad.
- Sin dependencias circulares ni acceso a internals de otros módulos.

## Reevaluación

Separar un componente solo cuando necesite escala, seguridad, disponibilidad, despliegue o ciclo de vida independiente.
