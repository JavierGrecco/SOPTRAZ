# Seguridad de rutas del almacén de confianza SQLite

El inicializador del trust store resuelve los alias del directorio padre una sola vez y entrega a SQLite la ruta resultante. Antes de abrir la base, valida el directorio padre y abre el componente final con semántica específica de cada sistema operativo para no seguir enlaces de forma silenciosa.

## Linux y macOS

- El archivo final se abre con `O_NOFOLLOW` y `O_NONBLOCK`; el descriptor se inspecciona con `fstat` y solo se acepta un archivo regular.
- El archivo queda protegido con modo `0600`, incluso cuando ya existía.
- Se recorren todos los ancestros desde `/` mediante descriptores `openat` con `O_NOFOLLOW`; cada uno debe ser un directorio real y se rechaza escritura de grupo/otros salvo sticky-bit en directorios compartidos protegidos como `/tmp`.
- No se cambian permisos de directorios padres existentes.

## Windows

- Se recorren todos los directorios desde la raíz del volumen y cada uno se abre con `FILE_FLAG_OPEN_REPARSE_POINT`; se rechazan reparse points y componentes que no sean directorios. En ancestros se rechazan permisos de borrado/reemplazo o cambio de ACL/owner otorgados a grupos amplios; permitir crear un nuevo hermano no se confunde con poder reemplazar un componente existente. En el archivo SQLite se conserva una comprobación de escritura más estricta.
- La DACL se consulta desde el handle abierto, no mediante una inspección textual del path.
- Se rechazan ACEs de escritura para Everyone, Authenticated Users, Builtin Users/Guests, Interactive, Network y Anonymous.
- Los ACEs extendidos/objeto/callback no soportados se rechazan de forma cerrada, en lugar de ignorarlos.
- No se reescribe ninguna ACL heredada o elegida por el administrador.

## Límites deliberados

El preflight final reduce el riesgo de sustitución, pero SQLite vuelve a abrir la ruta canónica por separado. No se afirma que esto elimine una carrera frente a otro proceso del mismo usuario que tenga permiso de escritura sobre el directorio padre. Por eso, los permisos y la integridad del árbol de directorios siguen siendo parte de la frontera de seguridad.

La comprobación de Windows cubre SIDs amplios conocidos; una instalación con grupos de dominio personalizados requiere revisión de sus DACL antes de producción. Un administrador local o un atacante con privilegios equivalentes al propietario del proceso queda fuera de la garantía de aislamiento local.

## Pruebas

Se añadieron pruebas para rechazar un archivo DB symlink y una carpeta en la ruta de DB, resolver alias del directorio padre y comprobar identidad del archivo sin depender de la forma textual de la ruta. En Unix se prueban directorios escribibles no protegidos y sticky-bit. En Windows se fuerza una DACL que concede control total a Everyone en el directorio inmediato y en un ancestro de un directorio privado; ambas rutas deben fallar cerradas. También se verifica que un ACE de creación de nuevos hermanos, sin derechos de borrado/reemplazo, no bloquee un perfil estándar.
