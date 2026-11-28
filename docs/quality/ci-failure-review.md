# Revisión de fallos de GitHub Actions

**Revisión:** 2026-11-27  
**Repositorio:** `JavierGrecco/SOPTRAZ`  
**Rama auditada:** `main`  
**Alcance:** 65 ejecuciones de Actions disponibles al 27 de noviembre de 2026, hasta el commit `079d240`.

## Resultado ejecutivo

Se revisaron 65 ejecuciones: **60 pushes del CI principal en main (24 fallidos y 36 exitosos), 1 PR de Dependabot exitoso y 4 ejecuciones dinámicas de Dependabot exitosas; no hubo cancelaciones**. Los runs #52–#54 comparten una regresión de construcción de `artifact.go`; #55 fue formato del fixture; #59 fue formato de una tabla de casos de CLI. Los runs #60 y #61 pasaron Windows, macOS, Ubuntu, Race Detector y fuzzing.

La revisión no atribuye cada job rojo a una causa distinta. Cuando tres runners fallan en el mismo paso con el mismo mensaje, se cuenta como una sola causa reproducida en tres plataformas.

## Inventario de ejecuciones fallidas

| Run | Commit | Job(s) que fallaron | Causa observada | Diagnóstico |
|---|---|---|---|---|
| [#1](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46057622504) | `7a21dd5` | Formato en Windows, macOS y Ubuntu | `internal/app/cli/cli.go` e `internal/platform/host/info.go` no estaban formateados | Defecto inicial de formato, no de lógica. Corregido con `gofmt` en `f1d4254`. Race Detector había pasado. |
| [#3](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46058756261) | `065b785` | `go vet` en las tres plataformas y Race Detector | `go: updates to go.mod needed; go mod tidy` | El módulo principal declaraba JSON Schema, pero no expresaba el requisito indirecto `golang.org/x/text v0.14.0` que exige el grafo del validador. Corregido en `8fb8f5f`. |
| [#6](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060274551) | `13203ed` | Formato/compilación en los tres sistemas y Race Detector | Los delimitadores de strings JSON quedaron escapados de forma inválida; el Race Detector también informó falta del checksum de `modernc.org/sqlite v1.63.0` | Dos defectos distintos: fixture Go mal generado y `go.sum` sin las dos entradas del módulo SQLite. |
| [#7](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060401041) | `8eda81c` | Formato/compilación y Race Detector | Persistían escapes incorrectos en los tests agregados; seguía sin estar completo el conjunto de checksums del driver SQLite | Repetición del defecto de fuente y de dependencias, no una regresión de la cadena de auditoría. |
| [#8](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060504326) | `ca50a1e` | Formato en los tres sistemas y Race Detector | El test de concurrencia contenía escapes sobrantes dentro de `fmt.Sprintf`; Race Detector seguía reclamando el checksum del driver | Repetición de los dos defectos previos. |
| [#9](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46060840889) | `3dfe5de` | Formato en los tres sistemas | `gofmt` señaló tres archivos: constantes alineadas, concatenación de SQL y una línea en blanco | La dependencia ya podía ejecutar el Race Detector; quedaba normalizar formato. Se añadió además el diagnóstico `gofmt -d` a CI. |
| [#10](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061012394) | `128db29` | `go test` en Windows | `invalid uri authority: C:` al abrir SQLite | Defecto funcional multiplataforma: `file://C:/...` colocaba la letra de unidad en la autoridad URI. Linux, macOS y Race Detector pasaron. |
| [#11](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061291236) | `1d33234` | Formato en los tres sistemas | El nuevo test tenía imports fuera del orden de `gofmt` | Defecto de formato posterior al arreglo de URI. Race Detector pasó. Se ordenaron imports en `ccc07a7`. |

La ejecución posterior al arreglo de imports es [#12](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061389071), commit `ccc07a7`: los cuatro jobs terminaron correctamente.

| [#13](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062376162) | `9cc3976` | Módulos en Ubuntu; `go vet` Windows/macOS; Race Detector | `go mod tidy -diff` quitó `github.com/google/pprof` y `modernc.org/fileutil` del `go.mod`; el helper de rutas no tenía imports para `runtime` y `strings` | El manifiesto incluía dos requisitos indirectos redundantes que pertenecen al grafo transitivo de SQLite y el helper de rutas no declaraba los paquetes `runtime` y `strings`. Se quitaron los requisitos redundantes y se incorporaron ambos imports. |
| [#14](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062502065) | `eda9fc3` | `go vet` Windows, macOS y Ubuntu; Race Detector | `store.go` contenía solo el bloque de imports (286 bytes): faltaban `Store`, `Open`, migraciones y funciones DSN | Error de integración introducido al reconstruir el blob: se creó una sustitución completa con solo imports en vez de modificar el bloque de imports. Restaurado el archivo completo desde el commit anterior y aplicado el import correcto en `8d677f5`. |
| [#23](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46065694412) | `125cd12` | Formato en Windows, macOS y Ubuntu | `internal/audit/checkpoint.go` no estaba formateado | Defecto de formato introducido al añadir las primitivas Ed25519. Race Detector y fuzzing pasaron. Corregido con `gofmt` en `bfe7f7b`; el run #24 volvió a verificarlo en los tres sistemas. |
| [#25](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46066212890) | `cea6909` | Tests en Windows, macOS, Ubuntu y Race Detector | Dos fixtures de regresión usaban `\\u0072ole` y `o\\u0077ner` (dos barras), que JSON interpreta como una barra literal seguida de texto, no como escapes Unicode equivalentes. | Error al generar los tests, no del decodificador: los nombres no eran duplicados después del parseo. Los logs muestran los mismos dos fallos en los cuatro jobs; fuzzing pasó. Se corrigieron los fixtures para usar un único escape JSON. |
| [#27](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46066682481) | `4f9a282` | Formato en los tres sistemas y Race Detector | `internal/strictjson/decode_test.go` contenía secuencias `\\t` y delimitadores de raw string escapados literalmente en las pruebas Unicode; `gofmt` informó `illegal character U+005C`. | Error de generación del archivo de pruebas, no del parser de producción. Los mismos defectos impidieron compilar en Windows, macOS, Ubuntu y Race Detector; el fuzzing separado pasó. La corrección reconstruye el bloque de pruebas como código Go normal y conserva los escapes JSON dentro de raw strings. |
| [#28](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46067113155) | `64205a3` | Control de formato en Windows, macOS y Ubuntu | `gofmt` encontró indentación de espacios en los nuevos tests de surrogate y una línea vacía redundante en `decode.go`. | La lógica ya compilaba: Race Detector y fuzzing pasaron. Se corrige únicamente formato, convirtiendo la indentación al estilo Go y eliminando la línea redundante. |
| [#33](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46068964686) | `92667a5` | Formato en tres sistemas, Race Detector y fuzzing | La refactorización conservó una segunda declaración de `CheckpointKeyID` y una prueba del resolver mantuvo el tipo anterior `ed25519.PublicKey`; también había tres archivos sin `gofmt`. | Defecto de integración del refactor, no cinco causas independientes. Se eliminó la duplicación, se migró el resolver y se reparó formato; #34 dejó pendiente solo la normalización de formato. |
| [#34](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46069340518) | `282adc0` | Formato en Windows, macOS y Ubuntu | `gofmt` detectó alineación de constantes, campos e imports de los tipos P-256. | El código compilaba: Race Detector y fuzzing pasaron. La corrección aislada llegó en `f55af4f` y fue aceptada en #35. |
| [#41](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46071331988) | `0c7e26d` | Formato en Ubuntu, macOS y Windows | `gofmt` detectó alineación de sentinels y campos del `Resolver` en `internal/modules/artifact/artifact.go`. | Defecto de formato de la primera entrega del resolver; Race Detector y fuzzing pasaron. Se aplicó `gofmt` sin cambiar lógica en `0d34763`; el run #42 dejó visible además una expectativa incorrecta en el test de ruta. |
| [#42](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46071468643) | `0d34763` | `go test` macOS y Windows; Ubuntu pasó | `TestResolveVerifiesInstalledArtifact` comparaba `ResolvedArtifact.Path` contra el path original de `t.TempDir()`. `NewResolver` usa `EvalSymlinks`, por lo que macOS devolvió `/private/...` en vez del alias `/var/...`; Windows también normalizó la ruta. | Fallo del fixture, no del hash/resolver: esperaba una ruta no canonicalizada. Race Detector y fuzzing pasaron. Corregido usando `filepath.EvalSymlinks(path)` para la expectativa; #43 validó el arreglo en los cinco jobs. |
| [#48](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46072865911) | `aee5ec2` | Formato en Windows, macOS y Ubuntu; Race Detector | La migración a `os.Root` dejó una separación en blanco faltante antes de un comentario (`gofmt`) y cinco pruebas usaban `err =` sin declarar la variable después de migrar al helper que crea el resolver. Race Detector detectó los mismos errores de compilación; fuzzing pasó. | Dos defectos de integración en los archivos nuevos del resolver/tests, no un fallo de `os.Root`. Se añadió `:=` en las pruebas, se corrigió formato y #49 validó los cinco jobs. |
| [#52](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46073840120) | `4b93557` | Formato en los tres sistemas y Race Detector | La edición que introdujo SemVer dañó la declaración de regex y duplicó parte del encabezado dentro de `artifact.go`; el parser Go informó `expected type`, `illegal character '?'` y declaraciones fuera de lugar. | Defecto de construcción del fuente, no del esquema JSON ni del resolver. Fuzzing pasó; los paquetes no pudieron compilar. |
| [#53](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46073939230) | `436344a` | Formato en Ubuntu/macOS y Race Detector | Persistía la corrupción de `artifact.go`; el commit cambió el esquema/tests pero no restauró el paquete Go. | Repetición de la misma regresión raíz de #52. |
| [#54](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46074231305) | `8be0986` | Formato/compilación y Race Detector | Un segundo intento de restauración volvió a insertar el texto del paquete dentro de la declaración de regex, dejando encabezados duplicados y sintaxis Go inválida. | Repetición del defecto de construcción automatizada. Se restauró desde `7c4177f`, último commit verde, y se verificó que hubiera un solo paquete y una sola función `Resolve` antes de publicar. |
| [#55](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46074536789) | `3e78fed` | Formato en Windows, macOS y Ubuntu; Race Detector y fuzzing pasaron | `gofmt` pidió alinear campos nuevos del fixture `testDescriptor`. | Defecto de formato de pruebas únicamente; producción compiló y Race Detector/fuzzing pasaron. Se ajustó el fixture y #56 verificó los cinco jobs. |
| [#59](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46075632934) | `cb3c5e1` | Formato en Windows, macOS y Ubuntu; Race Detector y fuzzing pasaron | `gofmt` pidió alinear los campos `name` e `invalid` del fixture de `TestValidateArtifactRejectsUnsafePathAndCredentials` en `internal/app/cli/cli_test.go`. | Defecto de formato del test, no de validación semántica; se alineó el campo y #60 cerró los cinco jobs. |

## Ejecuciones exitosas de referencia

| Run | Commit | Resultado y utilidad |
|---|---|---|
| [#2](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46057813655) | `f1d4254` | Windows, macOS, Ubuntu y Race Detector verdes después de corregir el formato de los archivos Go iniciales. |
| [#4](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46058857315) | `8fb8f5f` | Los cuatro jobs verdes después de completar el grafo de dependencias de JSON Schema. |
| [#5](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46059065782) | `2cdc448` | Los cuatro jobs verdes en la base previa a la implementación persistente SQLite. |
| [#12](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46061389071) | `ccc07a7` | Los cuatro jobs verdes después de corregir la URI de archivos SQLite en Windows. |
| [#15](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46062674995) | `8d677f5` | Los cuatro jobs verdes después de restaurar el archivo completo store.go; incluye módulo, formato, vet, pruebas multiplataforma y Race Detector. |
| [#16](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46063268875) | `e9ac1e6` | Los cuatro jobs verdes tras fijar las acciones por SHA, ejecutar tests sin caché y ampliar los casos SQLite para UNC, caracteres reservados, REPLACE y cabecera manipulada. |
| [#17](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46063349335) | PR Dependabot #2 | Los cuatro jobs verdes al probar setup-go v7.1.0 antes de integrarlo. |
| [#18](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46063592417) | `0787433` | Los cuatro jobs verdes tras actualizar la documentación de la auditoría de CI. |
| [#19](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46063747648) | `b71fbe9` | Los cuatro jobs verdes tras integrar setup-go v7.1.0 en main. |
| [#20](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46064474486) | `074c47f` | Los cuatro jobs verdes tras incorporar ADR-0013 y el diseño de checkpoint firmado / bundle Field → Hub. |
| [#21](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46065064799) | `268b6cd` | Los cinco jobs verdes: módulo, formato, vet, tests Windows/macOS/Ubuntu, Race Detector y fuzz de payloads JSON (16.810 ejecuciones, 66 casos interesantes nuevos, sin fallo). |
| [#22](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46065386846) | `cb3c381` | Los cinco jobs verdes tras actualizar la revisión de CI y el estado del endurecimiento de payloads. |
| [#24](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46065887175) | `bfe7f7b` | Los cinco jobs verdes después de aplicar `gofmt` a las primitivas de checkpoint Ed25519. El run #23 anterior había fallado solo en formato; Race Detector y fuzzing ya estaban verdes. |
| [#26](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46066404539) | `0596d38` | Los cinco jobs verdes después de corregir los dos fixtures de nombres Unicode escapados que habían fallado en #25. |
| [#29](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46067291694) | `d4f5cd3` | Los cinco jobs verdes tras normalizar los tests de surrogate UTF-16. |
| [#30](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46067561755) | `0ec0e0a` | Los cinco jobs verdes al rechazar claves JSON duplicadas en eventos SQLite persistidos. |
| [#31](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46067979232) | `4043e4b` | Los cinco jobs verdes al verificar snapshots consistentes de SQLite durante Append concurrente. |
| [#32](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46068406386) | `d0596e1` | Los cinco jobs verdes al construir checkpoints desde `VerifiedSnapshot`. |
| [#35](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46069664170) | `f55af4f` | Los cinco jobs verdes; normalización de formato que cierra el run #34. |
| [#36](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46069907829) | `9a6ed42` | Los cinco jobs verdes; la firma generada se verifica contra la clave pública devuelta por el signer. |
| [#37](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46070417666) | `1179761` | Los cinco jobs verdes al exponer `validate checkpoint` y admitir ambas firmas en manifiestos estables. |
| [#38](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46070597217) | `02fdea0` | Los cinco jobs verdes tras documentar ADR-0014 y sincronizar el modelo criptográfico. |
| [#39](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46070833159) | `4962b84` | Los cinco jobs verdes tras sincronizar roadmap, límites de módulos y revisión de CI hasta #38. |
| [#40](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46071004167) | `5a86713` | Los cinco jobs verdes; se rechazan parámetros incompletos de P-256 sin panic. |
| [#43](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46071637678) | `3d264d1` | Los cinco jobs verdes tras comparar la ruta esperada del test en forma canonicalizada. |
| [#44](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46071816981) | `03faaa6` | Los cinco jobs verdes; se rechazan componentes de ruta no portables/reservados de Windows en cualquier plataforma. |
| [#45](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46072264269) | `cb69391` | Cinco jobs verdes tras documentar resolver y puerta segura de integración. |
| [#46](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46072307338) | `5f082cd` | Cinco jobs verdes tras sincronizar roadmap y revisión de CI hasta #44. |
| [#47](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46072591187) | `88578f7` | Cinco jobs verdes tras añadir confinamiento de rutas bajo la raíz administrada. |
| [#49](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46073046521) | `293ca99` | Cinco jobs verdes al migrar a `os.Root` y sincronizar `Close` con resoluciones concurrentes. |
| [#50](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46073235441) | `179a741` | Cinco jobs verdes; cierre idempotente, rechazo de uso posterior y test concurrente aprobado por Race Detector. |
| [#51](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46073431042) | `7c4177f` | Cinco jobs verdes; la API pública deriva la plataforma real y valida combinaciones SO/arquitectura. |
| [#56](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46074662090) | `a994401` | Cinco jobs verdes tras restaurar el resolver desde el último commit verde, integrar descriptor v1 y validar SemVer/tamaño con fixture formateado. |
| [#57](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46075075169) | `fb70b55` | Cinco jobs verdes tras sincronizar contrato de artefactos, arquitectura y roadmap. |
| [#60](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46075788299) | `ecf0c01` | Cinco jobs verdes; la CLI combina JSON Schema con validación semántica Go y rechaza traversal y URLs con credenciales. |
| [#61](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46076562159) | `079d240` | Cinco jobs verdes con Go 1.27.4; se prueba rechazo de rutas terminadas en `/`. |

## Ejecuciones adicionales del PR #48 (2026-11-28)

Estas ejecuciones son posteriores al alcance histórico de 65 runs cerrado el 27-11-2026; se registran por separado y no cambian aquel recuento.

| Run | SHA | Resultado | Causa / verificación |
|---|---|---|---|
| [#346](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46167386331) | `10a52eb` | Falló formato | Cuatro archivos Go nuevos tenían líneas vacías redundantes y el mapa de esquemas necesitaba alineación `gofmt`. Fuzzing e higiene pasaron; la matriz de plataformas se detuvo en el paso de formato. |
| [#347](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46167477978) | `28aee40` | **6/6 aprobados** | Se corrigió formato y pasaron Windows, macOS, Ubuntu, Race Detector, fuzzing e higiene. Esta ejecución es previa al último ajuste de un fixture de control-character. |
| [#348](https://github.com/JavierGrecco/SOPTRAZ/actions/runs/46167625438) | `cf28693` | **6/6 aprobados** | Head actual del PR #48: Windows, macOS, Ubuntu, Race Detector, fuzzing e higiene. Incluye la prueba que inyecta un carácter de control real en un argumento fijo y verifica su rechazo. |

El primer intento #346 no se cuenta como aprobación. La validación final del PR #48 es #348, ejecutada sobre el SHA exacto `cf28693af55d148078da0a03024ed8d9913b3d90`.

## Causas raíz y correcciones permanentes

### 1. Formato Go

`gofmt -l` encontró archivos no normalizados en la primera base, y luego en los tests nuevos. Los errores de parser causados por los escapes inválidos se presentaban dentro de la misma puerta de formato, pero no eran un simple cambio estilístico: el compilador no podía analizar el fuente. El run #28 volvió a detectar exclusivamente formato en el bloque final de `decode_test.go` y una línea vacía extra en `decode.go`.

**Corrección permanente:** CI conserva la comprobación y ahora imprime `gofmt -d .` para exponer el diff exacto. No se deshabilitó ni se relajó el paso.

### 2. Grafo de módulos y checksums Go

El texto `go: updates to go.mod needed; to update it: go mod tidy` significa que el manifiesto declarado no describe el conjunto de requisitos que el cargador necesita para construir el módulo. El run #3 mostró que JSON Schema v6.0.3 necesitaba `golang.org/x/text v0.14.0`, que se incorporó. Los runs #6 a #8 mostraron que faltaban las dos sumas del módulo `modernc.org/sqlite v1.63.0`. El run #13 detectó el caso inverso: `github.com/google/pprof` y `modernc.org/fileutil` estaban declarados redundantemente aunque ya pertenecían al grafo transitivo del driver.

**Corrección permanente:** CI ejecuta `go mod tidy -diff` en Ubuntu. Ese modo compara el resultado de tidy sin modificar `go.mod` ni `go.sum`, y falla si hay diferencias. También ejecuta `go mod verify` para comprobar los módulos descargados.

### 3. Construcción de código de prueba y helpers

Los primeros tests de SQLite se construyeron con delimitadores JSON escapados de forma incorrecta, y después quedó un doble escape en el JSON generado por `fmt.Sprintf`. Las salidas exactas fueron `illegal character U+005C` y errores sintácticos derivados, antes de que comenzaran las pruebas SQLite. El run #25 repitió el patrón por escapes Unicode mal generados; el #27 lo repitió por secuencias `\\t` y delimitadores raw mal escapados en el nuevo test de Unicode. En ambos casos falló el código de prueba, no el parser; el run #26 corrigió #25. Más adelante, en el run #13, el nuevo helper de rutas llamó a `runtime.GOOS` y a `strings.ReplaceAll`/`strings.HasPrefix` sin importar los paquetes `runtime` y `strings`; `go vet` y Race Detector lo detectaron en compilación.

**Corrección permanente:** los fixtures usan strings Go válidos, el helper declara sus imports y los casos de concurrencia se compilan y pasan con Race Detector y en los tres runners.

### 4. URI de SQLite en Windows

El código original usaba `url.URL{Scheme: "file", Path: "C:/..."}`. `net/url` serializa el path sin barra inicial como `file://C:/...`; SQLite interpreta `C:` como autoridad y lo rechaza. SQLite espera la letra de unidad dentro del path: `file:///C:/...`.

**Corrección permanente:** el path se normaliza antes de serializarlo. Se cubren rutas con unidad Windows, unidad con barras, UNC, POSIX, caracteres reservados y rutas relativas; además, hay un test de integración que abre, escribe y verifica una base desde una ruta relativa real.

### 5. Sustitución accidental de un archivo fuente completo

El run #14 fue introducido por una corrección de integración nuestra: el blob de internal/storage/sqliteaudit/store.go se construyó con el bloque de imports (286 bytes) y se publicó como si fuera el archivo completo. Por eso los tres jobs go vet y Race Detector reportaron identificadores inexistentes como Store, Open, ErrCorruptChain y helpers DSN. No fue un problema de SQLite ni de los runners: el archivo había perdido realmente su implementación.

**Corrección permanente:** se restauró el archivo completo desde el commit anterior, que conservaba el ledger, y se aplicaron únicamente los imports necesarios en 8d677f5. El run #15 validó esa restauración en Windows, macOS, Ubuntu y Race Detector. Para evitar repetirlo, las correcciones deben operar sobre el archivo completo recuperado, inspeccionar el diff y comprobar la presencia de definiciones críticas antes de crear el commit.

## Contraste con documentación y proyectos expertos

1. La [referencia oficial de módulos Go](https://go.dev/ref/mod#go-mod-tidy) explica que `go mod tidy` agrega requisitos y checksums necesarios, y documenta `-diff` como comprobación de solo lectura que devuelve un código no cero cuando hay diferencias.
2. La [documentación oficial de URI de SQLite](https://www.sqlite.org/uri.html) especifica que una autoridad no vacía solo puede ser vacía o `localhost` (salvo compilaciones especiales con soporte UNC) y que las rutas Windows absolutas usan el patrón `/X:/...`.
3. El proyecto [Dicklesworthstone/beads_viewer, issue #198](https://github.com/Dicklesworthstone/beads_viewer/issues/198) reportó exactamente `invalid uri authority: E:%5C...`. Su autor confirmó la reproducción y la corrigió en [commit 5845a0e](https://github.com/Dicklesworthstone/beads_viewer/commit/5845a0e112c42fd865193e585d7b1e752992ee71), usando una función de normalización aislada y tests para drive, UNC, relativo y POSIX. SOPTRAZ adopta esa estrategia de normalización y amplía la verificación con apertura y escritura real.
4. El propio [modernc.org/sqlite v1.63.0](https://github.com/modernc-org/sqlite/tree/v1.63.0) documenta que el driver es pure Go/no-CGO, pero distribuye un grafo transitivo y componentes SQLite generados. Por eso se deben mantener checksums, SBOM y avisos de terceros antes de empaquetar Field.
5. La [guía de Go Modules](https://go.dev/wiki/Modules) recomienda `go mod tidy` para poner el manifiesto en un estado coherente y recuerda que tidy considera tests, combinaciones de plataforma y build tags, no solo el build que está corriendo en el SO actual.
6. La [documentación oficial de Go para `encoding/json`](https://pkg.go.dev/encoding/json) confirma que el decodificador tradicional sustituye UTF-8 inválido y pares sustitutos UTF-16 inválidos por U+FFFD; el [RFC 8259, sección 8.2](https://www.rfc-editor.org/rfc/rfc8259#section-8.2) advierte que el resultado de procesar sustitutos aislados es impredecible. El paquete interno `strictjson` rechaza esos escapes, además de claves duplicadas, UTF-8 inválido, documentos concatenados y profundidad excesiva.

## Controles de CI reforzados tras la auditoría

- `go mod tidy -diff` y `go mod verify` se ejecutan como puerta temprana en Ubuntu. `tidy -diff` carga también tests, tags y archivos específicos de plataforma según la semántica de Go Modules, por lo que no se necesita repetirlo en los tres SO.
- Las acciones de CI pasan de referencias por tag a SHAs completos verificados: `actions/checkout` v7.0.1 y `actions/setup-go` v7.1.0. Checkout usa `persist-credentials: false` porque el workflow solo necesita leer el repo.
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

Las 65 ejecuciones examinadas corresponden al historial disponible del repositorio al 2026-11-27: 60 pushes, un PR de Dependabot y cuatro ejecuciones dinámicas. El run #17 corresponde al PR; los cuatro runs dinámicos se identifican por separado y no se mezclan con los pushes. No se afirma que se hayan ejecutado pruebas locales en el Mac del usuario. Los runs #52–#54 se cuentan como una regresión raíz de construcción de `artifact.go`; #55 fue formato del fixture; #59 fue formato de la tabla de casos de CLI. La evidencia proviene de GitHub runners y de los logs asociados a los commits citados.

