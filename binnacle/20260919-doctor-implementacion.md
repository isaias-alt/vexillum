# 2026-09-19 - Implementación real de `vexillum doctor`

## Resuelto y en pie

- **Política de tmux (L1-10): opcional, warning, exit 0.** tmux es backend
  de control/comparación (herdr es primario, ver AGENTS.md). Su ausencia
  marca la línea como `missing` con la aclaración "(optional control
  backend)" pero no afecta el exit code ni el estado "ready" del resumen
  final. El resto de los chequeos (Claude Code, herdr, `~/.vexillum/`,
  proyecto inicializado, git repo) son obligatorios: si falta alguno, exit
  1 y el resumen dice "Environment not ready".

- **Chequeo de git repo en `doctor` es obligatorio**, consistente con la
  política elegida para `init` en la sesión anterior (L1-05: falla fuera de
  git). Reutiliza `isGitRepo` de `internal/cli/init.go`.

- **Chequeo de "proyecto inicializado" (L1-11)** reutiliza
  `projectAlreadyInitialized` (misma fuente de verdad que usa `init`:
  existencia de `.vexillum/config.json`). Si falta, la línea sugiere
  `vexillum init`.

- **Chequeo de `~/.vexillum/` escribible**: sin syscalls específicas de
  plataforma ni dependencias externas — se asume que el usuario actual es
  dueño de su propio `$HOME` (caso casi universal) y se mira solo el bit de
  escritura del owner (`mode.Perm()&0o200`). Decisión de simplicidad: no
  vale la pena resolver UID/GID del dueño real del directorio para un caso
  borde que prácticamente no ocurre en la práctica.

- **`doctor` es estrictamente de lectura** (L1-12): todos los chequeos usan
  `exec.LookPath` (no ejecuta los binarios) y `os.Stat` / `git rev-parse
  --is-inside-work-tree` (no escribe nada). Test dedicado
  (`TestDoctor_ReadOnly`) que saca una foto del árbol de archivos del
  proyecto y de `~/.vexillum/` antes y después de correr `doctor` y
  compara que sea idéntica.

- **Formato de salida**: una línea por chequeo (`[ok] <nombre>` o
  `[missing] <nombre> - <detalle>`), en el orden del PRD (Claude Code,
  herdr, tmux, `~/.vexillum/`, proyecto inicializado, git repo), seguido de
  una línea en blanco y el resumen final ("Environment ready.",
  "Environment ready (optional: tmux missing).", o "Environment not ready,
  see missing checks above.").

- **7 tests unitarios** (`internal/cli/doctor_test.go`, L1-07 a L1-13) que
  controlan qué binarios "existen" fijando `$PATH` a un directorio temporal
  con stubs ejecutables (`fakeBinDir`). Detalle importante: ese directorio
  siempre incluye un symlink al `git` real (resuelto antes de pisar
  `$PATH`), porque tanto el helper `initGitRepo` como el propio chequeo de
  git de `doctor` necesitan un git funcional sin importar qué binario esté
  bajo prueba en cada caso - de lo contrario `t.Setenv("PATH", ...)` deja
  a git inalcanzable y rompe los tests que sí esperan que sea un repo
  válido.

- **Verificación E2E manual** en un proyecto scratch fuera de este repo
  (mismo criterio de la sesión anterior: no correrlo sobre el propio repo
  de vexillum). Se corrió contra el PATH real de la máquina (que tiene
  `claude`, `herdr` y `git` instalados vía Homebrew, pero no `tmux`) y el
  resultado coincidió exactamente con lo esperado en los tres escenarios
  ejercitados a mano (sin scaffold, con scaffold, fuera de git repo).

- **Build, vet, gofmt y tests verdes**: 13 tests en total (6 de `init` + 7
  de `doctor`).

## Pendiente para la próxima

- **Capa 1 queda funcionalmente completa** (`init` + `doctor`, casos L1-01
  a L1-13). Falta decidir si conviene sumar tests explícitos para L1-14
  (comando desconocido) y L1-15 (help), hoy cubiertos por
  `cmd/vexillum/main.go` pero sin test automatizado.
- **Siguiente paso natural: Capa 2 (estado en disco)** - structs a JSON en
  `~/.vexillum/`, según `docs/prd-v1.md` y los criterios de alto nivel en
  `docs/test-cases.md`. Antes de arrancar, conviene decidir con el general
  qué primer struct modela (mission o scout) y bajar esos criterios de
  alto nivel a casos concretos con el mismo formato que la Capa 1.
- **Repo git de vexillum sigue sin primer commit**: `git status` en la raíz
  todavía muestra todo como `??` (untracked). No bloquea nada, pero
  conviene resolverlo antes de que crezca más el código sin historia
  versionada.
