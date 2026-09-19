# 2026-09-19 - Implementación real de `vexillum init`

## Resuelto y en pie

- **`vexillum init` fuera de un repo git: falla.** Decisión explícita pedida
  por L1-05: si el directorio actual no es un repo git, `init` no hace nada,
  imprime un mensaje claro a stderr y sale con exit code 1. Se prefirió sobre
  "inicializar con warning" porque las capas de worktrees (Capa 3+) van a
  requerir git de forma dura; no tiene sentido dejar un estado que después no
  se puede usar. Detección: `git rev-parse --is-inside-work-tree` corrido en
  el directorio del proyecto (`internal/cli/init.go:isGitRepo`); si `git` no
  está instalado o el comando falla, se trata igual como "no es un repo".

- **Orden de operaciones de `init`** (`internal/cli/init.go:runInit`):
  1. Chequea git repo (falla temprano, no toca nada si no lo es).
  2. Asegura `~/.vexillum/` (crea si falta). Si falla por permisos, reporta
     el path exacto y el error, exit 1, sin tocar el scaffold local
     (satisface L1-06: no deja estado a medias).
  3. Si ya existe `.vexillum/config.json` en el proyecto, se considera
     inicializado: informa y no toca nada más (satisface L1-02 y, como
     consecuencia, L1-03: si el proyecto ya está inicializado, `AGENTS.md`
     nunca se vuelve a mirar, así que una edición a mano queda intacta).
  4. Si no está inicializado: crea `.vexillum/config.json` (JSON con
     `version` y `initialized_at`, sin escritura atómica todavía — eso
     arranca en Capa 2 según AGENTS.md) y crea `AGENTS.md` de producto solo
     si no existe ya (si existiera por otra vía, se deja intacto y se
     informa).
  - Este orden resuelve L1-04 "gratis": si el scaffold local ya existe pero
    `~/.vexillum/` fue borrado a mano, el paso 2 lo recrea antes de llegar al
    chequeo de "ya inicializado" en el paso 3, sin tocar nada del proyecto.

- **AGENTS.md de producto (contenido inicial)**: template mínimo en inglés
  (`internal/cli/init.go:productAgentsMD`) que define el vocabulario del
  dominio (commander, soldiers, mission, scout, camp, sentinel) y aclara que
  el ruteo real todavía no existe (Capa 1). Se escribe tal cual la primera
  vez; el usuario lo edita libremente después y vexillum nunca lo vuelve a
  tocar una vez que existe.

- **Testeable sin tocar el HOME real**: `Init(args)` (wrapper de CLI) resuelve
  `cwd`/`$HOME` y llama a `runInit(projectDir, vexillumHome, stdout, stderr)`,
  que recibe ambos paths y los writers como parámetros. Los 6 tests de
  `internal/cli/init_test.go` (L1-01 a L1-06) usan `t.TempDir()` para
  proyecto y home falso, sin ensuciar el filesystem real. `go test ./...`,
  `go vet ./...` y `gofmt -l .` verdes.

- **Verificación E2E manual fuera de este repo**: se compiló el binario y se
  corrió contra un proyecto scratch con `git init` propio (no contra el repo
  de vexillum, porque acá `AGENTS.md` ya es el de gobierno del propio
  proyecto, no el de producto — correrlo acá sería confuso aunque el código
  es idempotente y no lo pisaría). Se ejercitaron a mano los 6 casos con
  salida y exit codes verificados uno por uno.

- **Nota sobre el `~/.vexillum/` real de esta máquina**: la primera corrida
  E2E se hizo sin fijar `$HOME`, así que creó de verdad
  `/Users/macuser/.vexillum/` (vacío). Es inofensivo e idempotente — es
  justo lo que se va a necesitar cuando vexillum se use en serio — así que
  se dejó en pie en vez de borrarlo. Las corridas siguientes usaron `$HOME`
  apuntando a directorios scratch para no seguir tocando el real.

## Pendiente para la próxima

- **Implementar `vexillum doctor` de verdad**: casos `L1-07` a `L1-13` de
  `docs/test-cases.md`. Depende de reusar `projectAlreadyInitialized` (ya
  escrita en `internal/cli/init.go`) para el chequeo "proyecto inicializado"
  de L1-11, y de la misma lógica de detección de git repo para L1-13.
  Política pendiente de decidir con el general: si tmux faltante es warning
  (exit 0) u obligatorio (exit != 0) — L1-10 lo deja abierto explícitamente.
- **L1-14 y L1-15** (comando desconocido, help) ya están cubiertos por el
  andamiaje de la sesión anterior (`cmd/vexillum/main.go`); no requieren
  trabajo nuevo, pero convendría sumarles tests explícitos cuando se toque
  `doctor`.
