# 2026-09-23 - Namespace por proyecto en ~/.vexillum

## Resuelto y en pie

- **Problema cerrado**: `tasks/` y `wakes/` eran globales por máquina.
  `Task`/`Wake` no tenían proyecto, y `sentinel.Drain` devolvía y ackeaba
  *todas* las wakes pendientes - con dos proyectos abiertos, el Stop hook
  del commander de uno consumía las wakes del soldier del otro.

- **Layout nuevo**: `~/.vexillum/projects/<key>/{tasks,wakes,camps}`.
  `<key>` (`"<repo>-<shortHash(absProject)>"`) vivía duplicada en
  `camp.Acquire` y `camp.Resolve` - extraída a `internal/project.Key`,
  paquete hoja nuevo (`internal/project/project.go`) sin dependencias más
  que stdlib, para que `camp`, `soldier`, `sentinel` y `cli` lo puedan
  importar sin ciclos. `project.Root(vexillumHome, projectDir)` resuelve
  la raíz namespaceada; `project.AllRoots(vexillumHome)` lista todos los
  proyectos que el sentinel debe barrer.

- **Normalización de symlinks en `project.Root`**: además de
  `filepath.Abs`, corre `filepath.EvalSymlinks` (con fallback silencioso a
  la ruta absoluta si falla) antes de hashear. Motivo: `vexillum dispatch`
  resuelve el proyecto desde `os.Getwd()` (preserva symlinks, como hace la
  shell), pero `vexillum sentinel drain` lo resuelve desde
  `git rev-parse --show-toplevel` (que sigue symlinks al buscar el
  toplevel) - sin normalizar, las dos rutas podían computar claves
  distintas para el mismo proyecto en una máquina donde su ruta involucra
  un symlink (el caso de `$TMPDIR` en macOS, `/var` → `/private/var` -
  exactamente el entorno de cualquier test con `t.TempDir()`). Verificado
  con `TestRoot_NormalizesSymlinks` (C1-04).

- **`camp.Acquire`/`camp.Resolve` sin cambio de firma**: solo cambió el
  cálculo interno de `poolRoot`, que pasa de `vexillumHome/<key>` a
  `project.Root(vexillumHome, projectDir)/camps`. El `shortHash` local
  duplicado en `camp.go` se borró.

- **`state.Save/Load/List` sin cambio de firma**: el parámetro (renombrado
  de `vexillumHome` a `projectRoot` para que el nombre no mienta) sigue
  siendo un string de raíz; lo que cambia es qué le pasa cada caller
  (antes `vexillumHome` a secas, ahora `project.Root(vexillumHome,
  projectDir)`).

- **`sentinel.Wake` pierde `Acked`**: `Drain` borra el archivo de la wake
  al entregarla en vez de reescribirlo con `Acked=true` - el campo no
  tenía otro lector, así que el borrado es la semántica más simple para
  "ya se entregó, no está más pendiente".

- **`sentinel.Tick(vexillumHome, client)` ahora barre todos los
  proyectos**: la lógica de un solo proyecto pasó a `tickProject(projectRoot,
  client)` (no exportada); `Tick` la llama una vez por cada raíz que
  devuelve `project.AllRoots(vexillumHome)`. Un error fatal en un proyecto
  corta el barrido del `Tick` completo (mismo criterio de corte que ya
  tenía un `Tick` de un solo proyecto) - el próximo `Tick`, 5s después,
  retoma. `sentinel.pid`/`sentinel.log` siguen 100% globales
  (`AcquireLock`/`IsRunning`/`Run` sin tocar): un sentinel por máquina, no
  uno por proyecto.

- **`soldier.RunInHerdr`/`soldier.Run` (headless) sin cambio de firma
  externa**: siguen tomando `vexillumHome`; internamente calculan
  `project.Root(vexillumHome, c.ProjectDir)` una sola vez al principio
  (`Camp` ya trae `ProjectDir`) y usan esa raíz para cada `state.Save`.
  `failHerdrTask` (no exportada) cambia su primer parámetro de
  `vexillumHome` a `projectRoot`.

- **`cli`: cada comando que toca tareas resuelve su propia
  `projectRoot`**: `runLand`/`runRelease`/`runRedispatch`/`runShip` ya
  tenían `projectDir` y `vexillumHome` en scope - cada uno agrega una
  línea `project.Root(vexillumHome, projectDir)` y la usa para
  `state.Load/Save`, mientras `camp.Acquire/Resolve` siguen recibiendo
  `projectDir, vexillumHome` sin cambios.

- **`vexillum sentinel drain`/`await` resuelven el proyecto desde el
  directorio actual**: `resolveDrainTarget(cwd, vexillumHome)` corre
  `git rev-parse --show-toplevel` con `cmd.Dir=cwd`; si `cwd` no está en
  un repo, o si el toplevel resulta estar bajo `vexillumHome` (es un
  camp), devuelve `("", nil)` - ni error, un no-op silencioso. `drain`
  imprime `{}` en ese caso; `await` sale con 0 de inmediato. Reusa
  `refuseInsideVexillumHome` (ya existía para `dispatch`/`land`/etc.) para
  el chequeo "¿está bajo vexillumHome?".

- **Bug real encontrado por el propio test C1-12/C1-13, arreglado antes de
  que pasaran**: `refuseInsideVexillumHome` comparaba el toplevel resuelto
  por git (sigue symlinks) contra un `vexillumHome` sin resolver - en el
  `$TMPDIR` symlinkeado de macOS, la comparación de prefijos fallaba en
  silencio y un camp real no se reconocía como tal (`resolveDrainTarget`
  devolvía un proyecto en vez de `("", nil)`). Arreglado normalizando
  `vexillumHome` con `filepath.EvalSymlinks` justo antes de esa
  comparación específica, dentro de `resolveDrainTarget`. En producción
  (`~/.vexillum` bajo `/Users/<usuario>`, normalmente sin symlinks) esto
  rara vez se manifestaría, pero es la misma clase de bug que motivó la
  normalización en `project.Root` - encontrado exactamente por escribir el
  test que el general pidió ("un drain desde un camp no consume wakes del
  proyecto"), no por inspección.

- **Verificado en el código, no en un test**: un soldier hereda el Stop
  hook que `vexillum init` escribe en `.claude/settings.json` solo si ese
  archivo está comiteado en el proyecto - `init` lo escribe pero nunca lo
  comitea, y `camp.Acquire` crea el worktree vía `git worktree add`, que
  solo ve lo que ya está en el commit. Confirma el paréntesis del general.

- **Casos de prueba nuevos**: `docs/test-cases.md`, sección "V3 - PARTE C
  (saneamiento) / C.1", C1-01 a C1-13. Tests: `internal/project/project_test.go`
  (paquete nuevo), reescritura de `internal/sentinel/sentinel_test.go` y
  `internal/cli/sentinel_test.go` (separan `vexillumHome` de `projectRoot`
  en cada caso existente, más los casos nuevos de `resolveDrainTarget`),
  ajustes mecánicos en `internal/cli/dispatch_test.go`,
  `land_merge_test.go`, `redispatch_test.go`, `ship_test.go` (guardan la
  tarea en `project.Root(home, project)` en vez de `home` a secas) y en
  `internal/soldier/herdr_run_test.go`/`soldier_test.go` (los `camp.Camp`
  de prueba ganan un `ProjectDir` fijo para poder recalcular la misma
  raíz al releer la tarea).

- **Sin migración**: tal como se pidió, ningún código ni test para el
  layout anterior (`~/.vexillum/tasks`, `~/.vexillum/wakes`,
  `~/.vexillum/<repo>-<hash>`) - el estado previo de la máquina del
  general se borra a mano antes del primer uso real de este cambio.

- Build, vet, gofmt y `go test -race ./...` en verde en todo el repo
  después de este cambio.
