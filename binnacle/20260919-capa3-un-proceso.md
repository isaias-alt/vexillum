# 2026-09-19 - Implementación de la Capa 3 (un proceso)

## Resuelto y en pie

- **Camp = slot de un pool reutilizable, no worktree creado/destruido por
  tarea.** Decisión explícita del general, inspirada en `treehouse`
  (confirmado leyendo su repo real vía `gh`, no de memoria): `Release`
  nunca destruye nada sucio, nunca devuelve al pool un branch no
  aterrizado (mergeado en la rama base), y verifica que quien libera el
  camp sea realmente su dueño. El worktree en sí nunca se borra - se
  devuelve limpio al pool para que la siguiente tarea lo reutilice
  (dependencias/build cache intactos). `internal/camp/camp.go`.

- **Sin dependencia en tiempo de ejecución de `treehouse`**: se reimplementó
  la lógica de pool en Go propio, no se shell-ea al binario `treehouse`.
  Consistente con AGENTS.md ("sin dependencias de runtime externas más
  allá de Claude Code, herdr, tmux") y con cómo `docs/references.md` ya
  enmarca a treehouse (referencia conceptual y de código, no dependencia).

- **Ubicación del pool**: `~/.vexillum/<nombre-proyecto>-<hash>/<slot>/<nombre-proyecto>`,
  con `hash` = primeros 8 hex de sha256 del path absoluto del proyecto
  (desambigua proyectos con el mismo nombre en distintas ubicaciones, igual
  que el esquema de treehouse).

- **Reuso de slot sin pisar la rama del proyecto principal**: al reutilizar
  un slot, el worktree se resetea con `git checkout --detach <base>` (HEAD
  desprendido) antes de crear la rama nueva de la tarea, en vez de
  `git checkout <base>` a secas. Necesario porque git rechaza checkoutear
  por nombre una rama que ya está checkouteada en otro worktree (la rama
  base suele estar checkouteada en el propio checkout principal del
  proyecto) - el mismo motivo por el que treehouse usa detached HEAD.

- **Rama base**: se resuelve como la rama actual del checkout principal del
  proyecto (`git rev-parse --abbrev-ref HEAD` en `projectDir`).
  Simplificación consciente respecto a treehouse (que resuelve contra
  `origin/HEAD` con fallback a `main`/`master` local) - no hay manejo de
  remotos todavía en el alcance de esta capa; documentado para revisar si
  hace falta más adelante.

- **`no-mistakes` / modos de proyecto (`direct-PR`, `local-only`): fuera de
  alcance**, confirmado con el general tras verificar en el repo real de
  `firstmate` (`docs/architecture.md`, sección "No-mistakes gate authority
  boundary") que es un subsistema propio y grande (archivo de config
  `.no-mistakes.yaml`, límite de autoridad separado, scripts dedicados) que
  no está en ningún doc de vexillum. Anotado como pendiente de decidir en
  `docs/prd-v1.md` (sección Capa 3), a resolver a más tardar en Capa 4
  (donde el PRD dice que "una mission entrega un PR").

- **`state.Task` pasa a schema v2** (`internal/state/task.go`): se agregan
  `CampSlot`, `CampPath`, `CampBranch`, `ExitCode *int`, `Output string`, y
  los estados `running`/`done`/`failed`. Bump de `SchemaVersion` de 1 a 2 -
  seguro porque no existe ningún archivo `v1` real en uso todavía (Capa 2
  nunca tuvo CLI que creara tareas fuera de tests).

- **`internal/soldier`**: `Run` persiste el `Task` en dos fases - `running`
  antes de arrancar el proceso, resultado final después - así un
  `vexillum` matado a mitad de camino deja el estado en disco reflejando
  honestamente que la tarea quedó interrumpida (verificado con un test que
  lee el estado *mientras* el proceso sigue corriendo, en una goroutine
  separada). Un exit distinto de 0 es un resultado esperado del soldier
  (se refleja en `Status`/`ExitCode`), no un error de Go; solo falla con
  error real cuando ni siquiera se pudo arrancar el proceso.

- **`soldier.ClaudeCommand` (decisión de seguridad, explícitamente
  confirmada por el general)**: la interfaz de `soldier.Run` es genérica
  (`CommandSpec` inyectable), pero la función de producción que arma el
  comando real invoca `claude -p "<prompt>" --dangerously-skip-permissions`.
  El general confirmó esto explícitamente, con el razonamiento de que el
  aislamiento del worktree acota el blast radius. Los tests de esta capa
  **nunca** invocan `claude` real - usan `CommandSpec` con comandos de
  prueba (`sh -c ...`); `ClaudeCommand` está implementada pero no se
  ejercita en ningún test automatizado ni se llama desde ningún lugar
  todavía (no hay CLI que la dispare).

- **`internal/atomicfile`**: se extrajo el patrón de escritura atómica
  (temp file + rename) a un paquete compartido, porque ya estaba
  duplicado entre `state.Save` (Capa 2) y el estado del pool de camps
  (Capa 3) - la tercera repetición real, no anticipada. `state.Save` se
  refactorizó para usarlo.

- **Sin CLI todavía**: igual que Capa 2, `internal/camp` e
  `internal/soldier` son librerías puras, sin subcomando nuevo de
  `vexillum`. Se verificó manualmente con un programa Go descartable
  (`go run` desde un directorio temporal dentro del módulo, borrado al
  terminar) que ejercitó el flujo completo extremo a extremo: crear tarea
  → adquirir camp → correr soldier → recargar desde disco → aterrizar
  (merge) → liberar camp → nueva tarea reutiliza el mismo slot. Coincidió
  exactamente con lo esperado.

- **9 casos concretos** (`L3-01` a `L3-09`) bajados en `docs/test-cases.md`
  a partir de los 6 criterios de alto nivel del PRD, mismo formato que
  capas anteriores. 10 tests nuevos (`internal/camp`: 6,
  `internal/soldier`: 4) los cubren.

- **Build, vet, gofmt y tests verdes**: 33 tests en todo el repo (13 de
  Capa 1 + 9 de Capa 2 + 6 de `internal/camp` + 4 de `internal/soldier` +
  1 subtest extra de round-trip en Capa 2, contado aparte por `go test`).

## Pendiente para la próxima

- **Decidir el modo de entrega de una mission** (equivalente a
  `no-mistakes`/`direct-PR`/`local-only` de firstmate) antes de que Capa 4
  necesite abrir un PR de verdad. Anotado en `docs/prd-v1.md`.
- **`soldier.ClaudeCommand` no está conectada a nada todavía**: nadie la
  invoca (no hay CLI ni commander). Cuando exista un caso de uso real, hay
  que decidir también el formato de salida (`--output-format`) y si
  conviene capturar JSON estructurado en vez de texto plano para
  `Task.Output`.
- **Resolución de rama base simplificada**: no contempla remotos
  (`origin/HEAD`). Revisar si hace falta antes de que haya proyectos con
  remoto real en juego.
- **Repo git**: sigue sin commitear esta capa. Falta decidir mensaje y
  confirmarlo con el general antes de commitear (mismo patrón que Capa 1 y
  Capa 2).
