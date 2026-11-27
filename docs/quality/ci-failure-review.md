# Revisión de fallos de GitHub Actions

**Revisión:** 2026-11-27  
**Repositorio:** `JavierGrecco/SOPTRAZ`  
**Rama auditada:** `main`  
**Alcance:** las 15 ejecuciones de CI creadas el 27 de noviembre de 2026 hasta el commit `8d677f5`.

## Resultado ejecutivo

Se revisaron las 15 ejecuciones registradas: **10 fallidas y 5 exitosas; no hubo ejecuciones canceladas**. Las fallidas se explican por seis causas raíz, algunas repetidas después de cada push. La última ejecución examinada, sobre el commit `8d677f5807ce07e993fe81ff59be51830b910c16` (run #15), terminó con los cuatro jobs en verde: Windows, macOS, Ubuntu y Go Race Detector.

La revisión no atribuye cada job rojo a una causa distinta. Cuando tres runners fallan en el mismo paso con el mismo mensaje, se cuenta como una sola causa reproducida en tres plataformas.

## Inventario de ejecuciones fallidas

| Run | Commit | Job(s) que fallaron | Causa observada | Diagnóstico |
|---|---|---|---|---|
| [#1](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46057622504) | `7a21dd5` | Formato en Windows, macOS y Ubuntu | `internal/app/cli/cli.go` e `internal/platform/host/info.go` no estaban formateados | Defecto inicial de formato, no de lógica. Corregido con `gofmt` en `f1d4254`. Race Detector había pasado. |
| [#3](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46058756261) | `065b785` | `go vet` en las tres plataformas y Race Detector | `go: updates to go.mod needed; go mod tidy` | El módulo principal declaraba JSON Schema, pero no expresaba el requisito indirecto `golang.org/x/text v0.14.0` que exige el grafo del validador. Corregido en `8fb8f5f`. |
| [#6](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060274551) | `13203ed` | Formato/compilación en los tres sistemas y Race Detector | Delimitadores de strings JSON quedaron como `\`` literales inválidos; el Race Detector también informó falta del checksum de `modernc.org/sqlite v1.63.0` | Dos defectos distintos: fixture Go mal generado y `go.sum` sin las dos entradas del módulo SQLite. |
| [#7](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060401041) | `8eda81c` | Formato/compilación y Race Detector | Persistían escapes incorrectos en los tests agregados; seguía sin estar completo el conjunto de checksums del driver SQLite | Repetición del defecto de fuente y de dependencias, no una regresión de la cadena de auditoría. |
| [#8](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060504326) | `ca50a1e` | Formato en los tres sistemas y Race Detector | El test de concurrencia contenía escapes sobrantes dentro de `fmt.Sprintf`; Race Detector seguía reclamando el checksum del driver | Repetición de los dos defectos previos. |
| [#9](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060840889) | `3dfe5de` | Formato en los tres sistemas | `gofmt` señaló tres archivos: constantes alineadas, concatenación de SQL y una línea en blanco | La dependencia ya podía ejecutar el Race Detector; quedaba normalizar formato. Se añadió además el diagnóstico `gofmt -d` a CI. |
| [#10](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061012394) | `128db29` | `go test` en Windows | `invalid uri authority: C:` al abrir SQLite | Defecto funcional multiplataforma: `file://C:/...` colocaba la letra de unidad en la autoridad URI. Linux, macOS y Race Detector pasaron. |
| [#11](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061291236) | `1d33234` | Formato en los tres sistemas | El nuevo test tenía imports fuera del orden de `gofmt` | Defecto de formato posterior al arreglo de URI. Race Detector pasó. Se ordenaron imports en `ccc07a7`. |

La ejecución posterior al arreglo de imports es [#12](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061389071), commit `ccc07a7`: los cuatro jobs terminaron correctamente.

| [#13](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062376162) | \`9cc3976\` | Módulos en Ubuntu; \`go vet\` Windows/macOS; Race Detector | \`go mod tidy -diff\` quitó \`github.com/google/pprof\` y \`modernc.org/fileutil\` del \`go.mod\`; el código nuevo llamaba a \`strings\` sin importarlo | El manifiesto incluía dos requisitos indirectos redundantes que pertenecen al grafo transitivo de SQLite y el helper de rutas no tenía el import de \`strings\`. Se quitaron los requisitos redundantes y se incorporó el import. |
| [#14](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062502065) | \`eda9fc3\` | \`go vet\` Windows, macOS y Ubuntu; Race Detector | \`store.go\` contenía solo el bloque de imports (286 bytes): faltaban \`Store\`, \`Open\`, migraciones y funciones DSN | Error de integración introducido al reconstruir el blob: se creó una sustitución completa con solo imports en vez de modificar el bloque de imports. Restaurado el archivo completo desde el commit anterior y aplicado el import correcto en \`8d677f5\`. |
| [#15](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062674995) | \`8d677f5\` | Ninguno | Todos los pasos pasaron en los cuatro jobs | Restauración validada. Race Detector, módulo, formato, vet y tests terminaron en verde. |

## Causas raíz y correcciones permanentes

### 1. Formato Go

`gofmt -l` encontró archivos no normalizados en la primera base, y luego en los tests nuevos. Los errores de parser causados por los escapes inválidos se presentaban dentro de la misma puerta de formato, pero no eran un simple cambio estilístico: el compilador no podía analizar el fuente.

**Corrección permanente:** CI conserva la comprobación y ahora imprime `gofmt -d .` para exponer el diff exacto. No se deshabilitó ni se relajó el paso.

### 2. Grafo de módulos Go

El texto `go: updates to go.mod needed; to update it: go mod tidy` significa que el manifiesto declarado no describe el conjunto de requisitos que el cargador necesita para construir el módulo. En este caso, JSON Schema v6.0.3 declara `golang.org/x/text v0.14.0`; se incorporó esa dependencia indirecta de manera explícita.

**Corrección permanente:** CI ejecuta `go mod tidy -diff` en Ubuntu. Ese modo compara el resultado de tidy sin modificar `go.mod` ni `go.sum`, y falla si hay diferencias. También ejecuta `go mod verify` para comprobar los módulos descargados.

### 3. Fixtures y generación de código de prueba

Los primeros tests de SQLite se construyeron con cadenas JSON delimitadas por backticks escapados de forma incorrecta, y después quedó un doble escape en el JSON generado por `fmt.Sprintf`. Las salidas exactas fueron `illegal character U+005C` y errores sintácticos derivados, antes de que comenzaran las pruebas SQLite.

**Corrección permanente:** los fixtures usan strings Go válidos; los casos de concurrencia se compilan y pasan con Race Detector y en los tres runners.

### 4. Integridad de dependencias

El primer `go.sum` copiado desde el upstream del driver SQLite incluía dependencias transitivas, pero omitía el checksum del propio `modernc.org/sqlite v1.63.0`. El import directo requiere ambas entradas: checksum del módulo y checksum de su `go.mod`.

**Corrección permanente:** se agregaron las entradas verificadas:
- `modernc.org/sqlite v1.63.0 h1:ClxazqRFNfr7XPHTVLbyzTVz2PjWPz+BfJNu2e8JDMs=`
- `modernc.org/sqlite v1.63.0/go.mod h1:Jzjh1mt4s0HuQ0KvtB1LIuHiGRmU2D5pgj8dUhW7/SE=`

El control `go mod tidy -diff` evitará que una futura omisión o suma sobrante pase inadvertida.

### 5. URI de SQLite en Windows

El código original usaba `url.URL{Scheme: "file", Path: "C:/..."}`. `net/url` serializa el path sin barra inicial como `file://C:/...`; SQLite interpreta `C:` como autoridad y lo rechaza. SQLite espera la letra de unidad dentro del path: `file:///C:/...`.

**Corrección permanente:** el path se normaliza antes de serializarlo. Se cubren rutas con unidad Windows, unidad con barras, UNC, POSIX, caracteres reservados y rutas relativas; además, hay un test de integración que abre, escribe y verifica una base desde una ruta relativa real.

## Contraste con documentación y proyectos expertos

1. La [referencia oficial de módulos Go](https://go.dev/ref/mod#go-mod-tidy) explica que `go mod tidy` agrega requisitos y checksums necesarios, y documenta `-diff` como comprobación de solo lectura que devuelve un código no cero cuando hay diferencias.
2. La [documentación oficial de URI de SQLite](https://www.sqlite.org/uri.html) especifica que una autoridad no vacía solo puede ser vacía o `localhost` (salvo compilaciones especiales con soporte UNC) y que las rutas Windows absolutas usan el patrón `/X:/...`.
3. El proyecto [Dicklesworthstone/beads_viewer, issue #198](https://github.com/Dicklesworthstone/beads_viewer/issues/198) reportó exactamente `invalid uri authority: E:%5C...`. Su autor confirmó la reproducción y la corrigió en [commit 5845a0e](https://github.com/Dicklesworthstone/beads_viewer/commit/5845a0e112c42fd865193e585d7b1e752992ee71), usando una función de normalización aislada y tests para drive, UNC, relativo y POSIX. SOPTRAZ adopta esa estrategia de normalización y amplía la verificación con apertura y escritura real.
4. El propio [modernc.org/sqlite v1.63.0](https://github.com/modernc-org/sqlite/tree/v1.63.0) documenta que el driver es pure Go/no-CGO, pero distribuye un grafo transitivo y componentes SQLite generados. Por eso se deben mantener checksums, SBOM y avisos de terceros antes de empaquetar Field.
5. La [guía de Go Modules](https://go.dev/wiki/Modules) recomienda `go mod tidy` para poner el manifiesto en un estado coherente y recuerda que tidy considera tests, combinaciones de plataforma y build tags, no solo el build que está corriendo en el SO actual.

## Controles de CI reforzados tras la auditoría

- `go mod tidy -diff` y `go mod verify` se ejecutan como puerta temprana en Ubuntu. `tidy -diff` carga también tests, tags y archivos específicos de plataforma según la semántica de Go Modules, por lo que no se necesita repetirlo en los tres SO.
- Las acciones de CI pasan de referencias por tag a SHAs completos verificados: `actions/checkout` v7.0.1 y `actions/setup-go` v7.0.0. Checkout usa `persist-credentials: false` porque el workflow solo necesita leer el repo.
- `GOTOOLCHAIN=local` obliga a usar el toolchain instalado por `setup-go`; no permite que el proceso cambie silenciosamente de toolchain.
- `go test -count=1` y `go test -race -count=1` fuerzan ejecución fresca en cada run.
- `cancel-in-progress: false` impide que un push nuevo cancele un CI que ya está validando un commit.
- Se mantienen límites de tiempo explícitos para jobs y actualización automática de dependencias vía Dependabot.

## Puertas de calidad que quedan activas

- Formato obligatorio; si falla, se muestra el diff.
- Módulo y checksums deben estar sincronizados (`go mod tidy -diff`).
- Los módulos descargados deben verificar su contenido (`go mod verify`).
- `go vet ./...` y `go test ./...` en Windows, macOS y Ubuntu.
- `go test -race ./...` en Ubuntu.
- No se aceptan fallos de una plataforma como "no aplicables": el objetivo de SOPTRAZ Field es Windows, macOS y Linux.

## Límites de esta auditoría

Las 15 ejecuciones examinadas corresponden al historial disponible del repositorio al 2026-11-27. La revisión no afirma que se hayan ejecutado pruebas locales en el Mac del usuario. La evidencia de compatibilidad descrita arriba proviene de los GitHub runners y de los logs asociados a los commits citados.
