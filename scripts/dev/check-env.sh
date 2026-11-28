#!/usr/bin/env bash
set -u

failures=0
warnings=0
ok()   { printf 'OK      %s\n' "$1"; }
warn() { printf 'AVISO   %s\n' "$1"; warnings=$((warnings + 1)); }
fail() { printf 'ERROR   %s\n' "$1"; failures=$((failures + 1)); }

printf 'SOPTRAZ — verificación del entorno de desarrollo\n'
printf 'Solo lectura: no instala, actualiza ni modifica herramientas.\n\n'

if ! command -v git >/dev/null 2>&1; then
  fail 'Git no está disponible en PATH.'
  repo_root=''
else
  git_version="$(git --version 2>/dev/null || true)"
  ok "$git_version"
  repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  if [ -z "$repo_root" ]; then
    fail 'No se detectó la raíz Git. Ejecutá este script dentro del repositorio.'
  else
    cd "$repo_root" || exit 1
    ok "Repositorio: $repo_root"
  fi
fi

if [ -n "${repo_root:-}" ] && [ ! -f "$repo_root/go.mod" ]; then
  fail 'No se encontró go.mod en la raíz del repositorio.'
fi

if [ -n "${repo_root:-}" ] && [ -f "$repo_root/go.mod" ]; then
  expected_go="$(awk '$1 == "go" { print $2; exit }' "$repo_root/go.mod")"
  if [ -z "$expected_go" ]; then
    fail 'No se pudo leer la directiva go de go.mod.'
  elif ! command -v go >/dev/null 2>&1; then
    fail 'Go no está disponible en PATH.'
  else
    go_version_output="$(go version 2>&1)"
    if [ $? -ne 0 ]; then
      fail "No se pudo ejecutar Go: $go_version_output"
    else
      actual_go="$(printf '%s\n' "$go_version_output" | sed -n 's/.* go\([0-9][0-9.]*\).*/\1/p')"
      if [ "$actual_go" = "$expected_go" ]; then
        ok "Go $actual_go coincide con go.mod."
      else
        fail "Go detectado: ${actual_go:-desconocido}; go.mod requiere $expected_go. No se instalará ni cambiará la versión automáticamente."
      fi

      gofmt_path="$(command -v gofmt 2>/dev/null || true)"
      if [ -n "$gofmt_path" ]; then
        ok "gofmt: $gofmt_path"
      else
        fail 'gofmt no está disponible en PATH.'
      fi

      platform="$(go env GOOS GOARCH 2>/dev/null | tr '\n' '/')"
      if [ -n "$platform" ]; then
        ok "Plataforma Go: ${platform%/}"
      else
        warn 'No se pudo consultar GOOS/GOARCH.'
      fi
    fi
  fi
fi

toolchain="${GOTOOLCHAIN:-}"
if [ "$toolchain" = "local" ]; then
  ok 'GOTOOLCHAIN=local'
elif [ -z "$toolchain" ]; then
  warn 'GOTOOLCHAIN no está definido; para reproducir CI usá: export GOTOOLCHAIN=local'
else
  warn "GOTOOLCHAIN=$toolchain; para reproducir CI usá GOTOOLCHAIN=local."
fi

if [ -n "${repo_root:-}" ] && command -v git >/dev/null 2>&1; then
  if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    warn 'El árbol de trabajo tiene cambios locales; preservalos antes de cambiar de rama o actualizar.'
  else
    ok 'Árbol de trabajo limpio.'
  fi
fi

printf '\nResultado: %s error(es), %s aviso(s).\n' "$failures" "$warnings"
if [ "$failures" -gt 0 ]; then
  printf 'Corregí los errores antes de ejecutar la suite local.\n'
  exit 1
fi
printf 'Entorno básico compatible. Ejecutá las pruebas indicadas en docs/development/local-development.md.\n'
exit 0
