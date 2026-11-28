# Desarrollo local de SOPTRAZ

Esta guía prepara el entorno para trabajar sobre el core actual de SOPTRAZ. Los comandos son deliberadamente explícitos: no se instala otra versión de Go automáticamente ni se modifica el equipo.

## 1. Requisitos actuales del core

- Git disponible en `PATH`.
- Go **exactamente en la versión indicada por `go.mod`**. Al corte de esta guía es `1.27.4`.
- `gofmt`, incluido en la distribución oficial de Go.
- Terminal: Bash en macOS/Linux o PowerShell en Windows.

Para este trabajo no hace falta instalar Node.js, pnpm, Python, Docker ni PostgreSQL. Se incorporarán cuando empecemos sus etapas (interfaz Field, motor analítico o Hub), con versiones y lockfiles propios.

Descargá Go desde [la página oficial](https://go.dev/dl/) eligiendo la versión fijada en **go.mod** y la arquitectura correcta. En Apple Silicon corresponde **darwin-arm64**; en una PC Windows habitual, **windows-amd64**, salvo que el hardware requiera otra arquitectura.

## 2. Clonar el repositorio

En una carpeta de trabajo:

```bash
git clone https://github.com/JavierGrecco/SOPTRAZ.git
cd SOPTRAZ
git switch main
git pull --ff-only
```

Si ya tenés un clon con cambios locales, no ejecutes `pull`, `switch` ni limpieza antes de revisar **git status** y preservar esos cambios.

## 3. Verificar el entorno

El verificador es de solo lectura: identifica Git, la raíz del repositorio, la versión de Go requerida, `gofmt`, la plataforma y el estado del árbol de trabajo. No instala ni actualiza herramientas.

### macOS y Linux

```bash
export GOTOOLCHAIN=local
bash scripts/dev/check-env.sh
```

### Windows PowerShell

```powershell
$env:GOTOOLCHAIN = "local"
./scripts/dev/check-env.ps1
```

Una versión de Go diferente a la fijada en **go.mod** es un error. `GOTOOLCHAIN=local` evita que el comando Go descargue otra toolchain automáticamente y permite reproducir la política de CI.

Los cambios locales se muestran como aviso, no como error: el verificador no descarta ni modifica trabajo del desarrollador.

## 4. Ejecutar la validación completa del core

Usá estos comandos desde la raíz del repositorio y con la versión correcta de Go.

### macOS y Linux

```bash
export GOTOOLCHAIN=local

go mod download
go mod verify
go mod tidy -diff

gofmt -l .
go vet ./...
go test -count=1 ./...

mkdir -p bin
go build -o ./bin/soptraz ./cmd/soptraz

./bin/soptraz version
./bin/soptraz host-info
./bin/soptraz device-profile
```

### Windows PowerShell

```powershell
$env:GOTOOLCHAIN = "local"

go mod download
go mod verify
go mod tidy -diff

gofmt -l .
go vet ./...
go test -count=1 ./...

New-Item -ItemType Directory -Force bin | Out-Null
go build -o .\bin\soptraz.exe .\cmd\soptraz

.\bin\soptraz.exe version
.\bin\soptraz.exe host-info
.\bin\soptraz.exe device-profile
```

`gofmt -l .` debe terminar sin listar archivos. Si lista archivos, no los formatees a ciegas junto a otros cambios: revisá el diff y aplicá `gofmt -w` solo a los archivos del cambio correspondiente.

La matriz CI ejecuta pruebas nativas en Windows, macOS y Ubuntu, Race Detector y fuzzing acotado en Ubuntu. La suite local complementa la matriz; no simula hardware de otros sistemas operativos.

## 5. Investigar fallos sin perder contexto

Cuando falle un comando:

1. No canceles la ejecución para lanzar otra después de introducir más cambios. Conservá la salida de la ejecución que falló.
2. Anotá el commit exacto: **git rev-parse HEAD**.
3. Guardá el comando, sistema operativo/arquitectura, versión de Go y el primer error completo.
4. Comprobá si el fallo es reproducible sin caché cuando corresponda: `go test -count=1 ./ruta/del/paquete`.
5. Antes de corregir, separá fallo de código, incompatibilidad de plataforma, dependencia de entorno y problema transitorio.
6. Hacé un cambio coherente, ejecutá primero la prueba del paquete afectado y después la suite completa.

No publiques logs que contengan credenciales, tokens, direcciones privadas u otros datos sensibles. Los informes técnicos locales de SOPTRAZ están diseñados para permanecer en el equipo; revisalos y sanitizalos antes de compartirlos.

## 6. Límites de esta guía

Esta guía prepara el core Go actual. Todavía no configura:

- frontend Wails/React y su toolchain Node/pnpm;
- empaquetado y firma de releases;
- servicios PostgreSQL/Hub;
- worker Python de analítica;
- laboratorios físicos/VM para acciones mutantes, drivers o recuperación.

Esas dependencias se agregarán en la etapa correspondiente, con instrucciones separadas y pruebas reproducibles. No conviertas herramientas futuras en prerrequisitos globales para desarrollar el core actual.
