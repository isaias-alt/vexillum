# 2026-09-22 - Diseño e implementación: reconciliación camp/PR después de ship

Sesión de diseño (vía Lavish Editor, artefacto `.lavish/ship-camp-reconciliation.html`) seguida de implementación. Origen: mientras se revisaba el estado de `internal/` para B.4 (no-mistakes/ship), el general notó que `ship` pushea la branch del camp pero no-mistakes puede aplicar auto-fixes en su propio worktree aislado, dejando el camp y el PR real potencialmente divergentes - un gap no contemplado en `docs/no-mistakes.md`.

## Resuelto y en pie

- **Investigación previa a diseñar**: se buscó cómo resuelve el mismo problema `github.com/kunchenguid/firstmate` (mismo autor que `no-mistakes`, mismo patrón commander/soldiers/worktrees, con más rodaje). Encontrado en `bin/fm-teardown.sh`: `content_in_default()` (mergea en memoria la base contra el HEAD del camp con `git merge-tree --write-tree` y compara árboles - detecta contenido ya aterrizado aunque el squash-merge rompa la ancestría de commits) y `pr_is_merged()` (consulta `gh pr view` en vivo + `git patch-id` para comparar contenido contra el head del PR, sobreviviendo un rebase). También `bin/fm-pr-merge.sh`: firstmate no solo se niega a mergear un task PR-based (`fm-merge-local.sh` rechaza `mode≠local-only`) - tiene un comando hermano que hace el merge real vía `gh pr merge --match-head-commit`, después de verificar en vivo que el PR está abierto, no draft, mergeable y todo verde.

- **Diseño cerrado en 3 rondas de feedback vía Lavish** (el general prefiere este medio a AskUserQuestion para decisiones de diseño con código de por medio):
  1. Primera ronda: el general pidió ver específicamente cómo lo hace firstmate, no solo mi resumen.
  2. Segunda ronda: el general eligió explícitamente la opción "vexillum mergea de verdad" (no solo negarse y dejarle el click al general) para el caso `land` sobre un task shippeado - señalando que el objetivo es ahorrarle trabajo manual.
  3. Tercera ronda: el general corrigió dos imprecisiones - (a) la tabla comparativa decía "omitido" para verificación live contra GitHub, cuando en realidad `land` (Opción B elegida) sí la usa; (b) preguntó si el `gh` de la propuesta era `gh-axi` (como quota-axi/lavish/chrome-devtools-axi) - no lo es, es el CLI oficial de GitHub llamado directo vía `os/exec`, sin juicio de LLM de por medio, distinto de `gh-axi` (que sigue fuera del catálogo activo de vexillum).

- **Cuatro cambios implementados** (commit pendiente de confirmación del general):
  1. `internal/state/task.go`: `StatusShipped` nuevo en el enum de `Status`.
  2. `internal/cli/ship.go`: acepta `done` o `shipped` como estado de entrada (reshippear empuja más commits al mismo PR); marca el task `shipped` tras un push exitoso.
  3. `internal/cli/land_merge.go` (archivo nuevo) + `internal/cli/dispatch.go` (`runLand`): sobre un task `shipped`, mergea el PR real vía `gh pr view` (verifica abierto/no-draft/mergeable) + `gh pr checks` (todo verde) + `gh pr merge --match-head-commit <head verificado> --squash` - versión mínima de `fm-pr-merge.sh`, sin la maquinaria de away-authority/captain-hold de firstmate (pensada para agentes concurrentes; vexillum tiene un solo commander). No se graba ningún dato del PR en el task - se resuelve por nombre de branch (`task.CampBranch`) en cada llamada.
  4. `internal/camp/camp.go` (`Release`): fallback `contentAlreadyInBase` cuando el chequeo de ancestro falla - mismo mecanismo que `content_in_default` de firstmate, git puro, corre siempre (no condicionado a `StatusShipped`), así que también arregla retroactivamente camps de missions shippeadas antes de este cambio.
  5. `internal/cli/doctor.go`: chequeo nuevo `checkGitHubCLI` (informativo, no bloqueante, mismo criterio que `no-mistakes`).
  6. `docs/no-mistakes.md`: sección nueva "Reconciliación camp/PR después de ship"; reescrita la frase de "No entra" que decía "mergear es responsabilidad del general" (sigue siendo cierto para `land` local, ya no para un task `shipped`); aclarado que esto usa `gh`, no `gh-axi`.

- **Verificación antes de codear**: se simuló un squash-merge real (`git merge --squash` + commit) en un repo de prueba y se confirmó con comandos git directos que `git merge-base --is-ancestor` da `false` (el bug) pero `git merge-tree --write-tree base feature` produce el mismo árbol que la base (el fix) - antes de escribir el código Go, para no confiar a ciegas en el mecanismo de firstmate sin probarlo en este entorno.

- **Tests nuevos, todos verdes** (`go build`, `go vet`, `gofmt -l`, `go test ./...` limpios):
  - `internal/cli/ship_test.go`: `TestRunShip_RecordsShippedStatus`, `TestRunShip_AllowsReshippingAShippedTask`.
  - `internal/cli/land_merge_test.go` (archivo nuevo): éxito, y refusals por PR cerrado, draft, no-mergeable, checks rojos (confirmando que el merge nunca se intenta si los checks fallan), y `gh` no instalado. Más `TestRunLand_ShippedTaskMergesPR` confirmando que `runLand` nunca intenta resolver un camp para un task shippeado.
  - `internal/cli/doctor_test.go`: `TestDoctor_GitHubCLINotInstalled`, `TestDoctor_GitHubCLIInstalled`.
  - `internal/camp/camp_test.go`: `TestRelease_AcceptsSquashMergedContent` (repro real del squash-merge) y `TestRelease_RefusesGenuinelyDivergedCamp` (negativo - confirma que el fallback no tapa trabajo genuinamente no aterrizado).

- **Complicación operativa durante la verificación en vivo del fix anterior de esta misma sesión (Stop hook), relevante para la próxima vez**: copiar (`cp`) un binario Go recién compilado sobre `/opt/homebrew/bin/vexillum` (path ya en uso) lo dejó no-ejecutable (`zsh: killed`, exit 137) **incluso restaurando el backup byte-a-byte idéntico** - algo a nivel de confianza de macOS/AMFI para binarios ad-hoc-signed en Apple Silicon se invalida con la reescritura del archivo, sin importar el contenido. Se resolvió pidiéndole al general que corriera `go build -o /opt/homebrew/bin/vexillum ./cmd/vexillum` directo (no `cp`) desde su propia terminal. Lección: nunca instalar un binario Go local sobre un path ya en uso con `cp` en macOS ARM - reconstruir con `go build -o <path>` directo ahí.

## Pendiente para la próxima

- **Commit y push todavía no confirmados por el general** - el mensaje de commit propuesto está en el chat, esperando luz verde antes de comitear.
- **Verificación E2E real completa queda pendiente**: los tests nuevos usan `git` real y un `gh`/`no-mistakes` stubeados, no un PR real de GitHub. Falta correr el flujo completo (`ship` → no-mistakes aplica un auto-fix real → `land` mergea el PR real vía `gh`) contra un repo de prueba real, similar a como se hizo con `vexillum-prueba` en la sesión anterior - no se hizo en esta sesión para no abrir/mergear un PR real sin pedirlo explícitamente primero.
- Los pendientes de sesiones anteriores siguen sin cambios: distribución brew/curl real, y el binario en `/opt/homebrew/bin/vexillum` sigue siendo una build manual, no gestionada por brew.
