$ErrorActionPreference = "Stop"

$failures = 0
$warnings = 0

function Write-Ok([string]$Message)   { Write-Host "OK      $Message" }
function Write-Warn([string]$Message) { Write-Host "AVISO   $Message"; $script:warnings++ }
function Write-Fail([string]$Message) { Write-Host "ERROR   $Message"; $script:failures++ }

Write-Host "SOPTRAZ — verificación del entorno de desarrollo"
Write-Host "Solo lectura: no instala, actualiza ni modifica herramientas."
Write-Host ""

$gitCommand = Get-Command git -ErrorAction SilentlyContinue
$repoRoot = $null

if ($null -eq $gitCommand) {
    Write-Fail "Git no está disponible en PATH."
} else {
    $gitVersion = (& git --version 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -eq 0) { Write-Ok $gitVersion }
    else { Write-Fail "No se pudo ejecutar Git." }

    $repoRoot = (& git rev-parse --show-toplevel 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($repoRoot)) {
        Write-Fail "No se detectó la raíz Git. Ejecutá este script dentro del repositorio."
        $repoRoot = $null
    } else {
        Write-Ok "Repositorio: $repoRoot"
    }
}

if ($null -ne $repoRoot) {
    $goModPath = Join-Path $repoRoot "go.mod"
    if (-not (Test-Path -LiteralPath $goModPath -PathType Leaf)) {
        Write-Fail "No se encontró go.mod en la raíz del repositorio."
    } else {
        $goMod = Get-Content -LiteralPath $goModPath -Raw
        $goDirective = [regex]::Match($goMod, '(?m)^\s*go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)\s*$')
        if (-not $goDirective.Success) {
            Write-Fail "No se pudo leer la directiva go de go.mod."
        } else {
            $expectedGo = $goDirective.Groups[1].Value
            $goCommand = Get-Command go -ErrorAction SilentlyContinue
            if ($null -eq $goCommand) {
                Write-Fail "Go no está disponible en PATH."
            } else {
                $goVersionOutput = (& go version 2>&1 | Out-String).Trim()
                if ($LASTEXITCODE -ne 0) {
                    Write-Fail "No se pudo ejecutar Go: $goVersionOutput"
                } elseif ($goVersionOutput -match '\bgo([0-9]+(?:\.[0-9]+){1,2})\b') {
                    $actualGo = $Matches[1]
                    if ($actualGo -eq $expectedGo) {
                        Write-Ok "Go $actualGo coincide con go.mod."
                    } else {
                        Write-Fail "Go detectado: $actualGo; go.mod requiere $expectedGo. No se instalará ni cambiará la versión automáticamente."
                    }

                    if ($null -ne (Get-Command gofmt -ErrorAction SilentlyContinue)) {
                        Write-Ok "gofmt disponible."
                    } else {
                        Write-Fail "gofmt no está disponible en PATH."
                    }

                    $platform = (& go env GOOS GOARCH 2>$null | Out-String).Trim() -replace '\r?\n', '/'
                    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($platform)) {
                        Write-Ok "Plataforma Go: $platform"
                    } else {
                        Write-Warn "No se pudo consultar GOOS/GOARCH."
                    }
                } else {
                    Write-Fail "No se pudo interpretar la versión de Go: $goVersionOutput"
                }
            }
        }
    }

    $gitStatus = (& git -C $repoRoot status --porcelain 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "No se pudo consultar el estado del repositorio."
    } elseif (-not [string]::IsNullOrWhiteSpace($gitStatus)) {
        Write-Warn "El árbol de trabajo tiene cambios locales; preservalos antes de cambiar de rama o actualizar."
    } else {
        Write-Ok "Árbol de trabajo limpio."
    }
}

if ($env:GOTOOLCHAIN -eq "local") {
    Write-Ok "GOTOOLCHAIN=local"
} elseif ([string]::IsNullOrWhiteSpace($env:GOTOOLCHAIN)) {
    Write-Warn "GOTOOLCHAIN no está definido; fijalo en local para reproducir CI."
} else {
    Write-Warn "GOTOOLCHAIN=$env:GOTOOLCHAIN; para reproducir CI usá GOTOOLCHAIN=local."
}

Write-Host ""
Write-Host "Resultado: $failures error(es), $warnings aviso(s)."
if ($failures -gt 0) {
    Write-Host "Corregí los errores antes de ejecutar la suite local."
    exit 1
}
Write-Host "Entorno básico compatible. Ejecutá las pruebas indicadas en docs/development/local-development.md."
exit 0
