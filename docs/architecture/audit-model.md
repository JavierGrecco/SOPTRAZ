# Modelo de auditoría de SOPTRAZ

**Estado:** primera implementación persistente para SOPTRAZ Field. Los checkpoints firmados, la exportación Field → Hub y la retención organizacional siguen pendientes.

## 1. Unidad de trazabilidad

La unidad es una **intervención**. Cada intervención mantiene una secuencia independiente de eventos:

`intervention.created → diagnostic.completed → approval.granted → operation.started → operation.completed → verification.completed → intervention.closed`

No todos los tipos deben aparecer en todas las intervenciones. Los tipos admitidos están definidos en `internal/contracts/schemas/intervention-event.schema.json`.

Cada evento registra:
- ID UUID.
- ID de intervención.
- Número de secuencia.
- Tipo de evento.
- Fecha y hora UTC.
- Actor (usuario, sistema, agente o IA).
- Payload JSON.
- Hash del evento anterior.
- Hash SHA-256 propio.

Los payloads deben ser UTF-8 válido y objetos JSON sin claves duplicadas, incluso si los nombres se repiten mediante escapes Unicode equivalentes. No deben incluir contraseñas, tokens, claves de cifrado ni otros secretos.

## 2. Encadenamiento de hashes

El cálculo utiliza SHA-256 y el separador de dominio:

`SOPTRAZ-AUDIT-EVENT-V1\0`

El preimage contiene, en orden estable, schemaVersion, eventId, interventionId, sequence, eventType, occurredAt, actor, payload canonicalizado y previousHash. El campo hash no se incluye en su propio preimage.

El payload se analiza recursivamente con UseNumber; la canonicalización rechaza claves duplicadas en todos los niveles y se vuelve a serializar con las claves de los objetos ordenadas por encoding/json. El hash se expresa en hexadecimal minúsculo.

El evento 1 no tiene previousHash. Cada evento posterior incorpora el hash del inmediatamente anterior de esa misma intervención.

**No cambiar el separador, el orden de campos ni la canonicalización dentro de la versión 1.0.0.** Si se necesita otra regla, introducir una versión de algoritmo/contrato nueva y pruebas de compatibilidad.

## 3. Persistencia Field

El adaptador inicial es `internal/storage/sqliteaudit`, basado en SQLite con modernc.org/sqlite:

- tabla `audit_events`: filas de eventos y secuencia única por intervención;
- tabla `audit_heads`: última secuencia y hash de cada intervención;
- triggers que rechazan UPDATE y DELETE sobre las filas de eventos, con `recursive_triggers=ON` para que `INSERT OR REPLACE` no pueda evitar el trigger de borrado;
- transacciones inmediatas para serializar escritores;
- pool de una conexión en Field;
- busy timeout de 5 segundos;
- synchronous FULL;
- claves foráneas activadas;
- verificación de cada enlace y de la cabecera final.

El motor de auditoría no depende directamente de SQLite: la interfaz `internal/audit.Store` permite un futuro adaptador PostgreSQL para Hub.

No se habilita WAL en esta primera edición. Se prioriza el modo de journaling predeterminado para simplificar el uso desde medios extraíbles. La estrategia de backup/checkpoint y los escenarios de extracción segura del dispositivo se probarán antes del lanzamiento portable.

## 4. Verificación

`Verify(interventionID)` comprueba:
1. que la secuencia empiece en 1 y no tenga huecos;
2. que todos los eventos pertenezcan a la intervención consultada;
3. que previousHash coincida con el evento anterior;
4. que cada evento cumpla el contrato JSON;
5. que el SHA-256 calculado coincida con el guardado;
6. que secuencia/hash finales coincidan con audit_heads.

`List` devuelve los eventos en orden, y también comprueba que la columna hash indexada coincida con el JSON almacenado. Quien consuma datos como evidencia confiable debe ejecutar Verify.

## 5. Límites de seguridad

Los triggers previenen modificaciones accidentales mediante las operaciones normales de base de datos; `recursive_triggers=ON` también permite bloquear `INSERT OR REPLACE` cuando su resolución de conflicto intenta borrar una fila existente. El hash chain permite detectar alteraciones parciales, cambios, huecos y truncamientos cuando la cabecera permanece intacta.

**Esto no convierte SQLite en almacenamiento WORM ni hace imposible una manipulación por un administrador con acceso al archivo.** Un atacante que pueda editar todas las tablas, quitar triggers y recalcular toda la cadena puede reescribir el historial completo.

Para elevar la garantía, se requiere exportar checkpoints del último hash a un destino separado y firmarlos con una clave protegida fuera de la base local. El [diseño M2 de checkpoints y paquetes](audit-checkpoint-design.md) especifica el modelo de confianza, firma, ancla independiente, anti-rollback y exportación/importación. La implementación permanece pendiente; no se presenta como disponible.

## 6. Field y Hub

Field guarda la auditoría local y debe poder verificarla sin red. Hub tendrá otro adaptador que implemente la misma interfaz, con persistencia PostgreSQL y reglas de idempotencia. La sincronización usará paquetes de intervención versionados; nunca se sincronizará copiando una base SQLite abierta a través de una red.
