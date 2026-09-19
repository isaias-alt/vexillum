# 2026-09-19 - Implementación de la Capa 2 (estado en disco)

## Resuelto y en pie

- **Struct único `Task` con campo `Kind`**, en vez de structs separados para
  mission y scout. Decisión del general: mission y scout comparten casi todo
  el estado (id, prompt, status, timestamps); solo difiere el resultado
  final (PR vs. reporte), que esta capa todavía no modela (no hay workers
  que lo produzcan). Un solo struct evita duplicar la lógica de
  serialización/lectura/listado antes de saber si realmente van a divergir.
  `internal/state/task.go:Task`, `Kind = "mission" | "scout"`.

- **Ubicación en disco**: `~/.vexillum/tasks/<id>.json`, un archivo por
  tarea. El id es aleatorio (8 bytes de `crypto/rand`, hex de 16
  caracteres), no secuencial, pensando en Capa 4 (creación concurrente sin
  coordinación de contador).

- **Escritura atómica desde ya** (`internal/state/task.go:Save`): escribe a
  un archivo temporal en el mismo directorio (`os.CreateTemp`) y hace
  `os.Rename` al nombre final. Cumple la regla de AGENTS.md de que la
  escritura de estado es atómica a partir de esta capa, aunque hoy se
  invoque a mano.

- **Versionado de esquema**: campo `schema_version` en el JSON
  (`state.SchemaVersion = 1`). `Load`/`List` rechazan con error claro
  cualquier archivo cuya versión no coincida con la soportada, en vez de
  interpretarlo como datos válidos.

- **Detección de corrupción sin panic**: JSON inválido, campos obligatorios
  faltantes (`id`, `kind`), o versión de esquema no soportada, todos
  producen un `error` envuelto con contexto (`fmt.Errorf("...: %w", err)`),
  nunca panic ni datos basura silenciosos.

- **`List` no ignora corrupción**: si cualquier archivo dentro de
  `tasks/` falla al decodificarse, `List` corta y devuelve el error
  nombrando el archivo problemático, en vez de devolver una lista parcial
  o saltear el archivo malo en silencio. Política elegida a propósito:
  ocultar corrupción sería peor que fallar ruidosamente.

- **Sin CLI todavía**: `internal/state` es un paquete de librería pura, sin
  ningún subcomando nuevo de `vexillum` que lo invoque. Consistente con el
  PRD ("el estado se crea y se lee a mano, invocado por el autor") y con no
  adelantar Capa 3/4 (no hay workers que creen tareas reales todavía). Se
  ejercita solo desde los tests.

- **Casos de prueba concretos**: se bajaron los 6 criterios de alto nivel de
  Capa 2 en `docs/test-cases.md` a `L2-01` a `L2-08`, mismo formato que
  Capa 1 (precondición/acción/esperado). 9 tests en
  `internal/state/task_test.go` los cubren (incluye round-trip para mission
  y scout como subtests, y un caso extra de unicidad de ids no numerado).

- **Build, vet, gofmt y tests verdes**: 22 tests en total en todo el repo
  (13 de Capa 1 + 9 de Capa 2).

## Pendiente para la próxima

- **Capa 3 (un proceso)**: lanzar un solo soldier en un camp (git worktree
  aislado), esperar y capturar resultado, persistir el resultado final
  usando `internal/state`. Acá recién aparece `os/exec` y la
  creación/teardown de worktrees. Antes de arrancar, bajar los criterios de
  alto nivel de `docs/test-cases.md` (sección Capa 3) a casos concretos,
  como se hizo con Capa 1 y Capa 2.
- **Campo de resultado en `Task` todavía no existe**: a propósito, se
  postergó hasta que Capa 3 defina qué forma tiene un resultado de mission
  (PR) vs. scout (reporte). Cuando se agregue, va a requerir subir
  `SchemaVersion` a 2 y decidir qué hacer con archivos `schema_version: 1`
  ya en disco (migración vs. rechazo).
- **Repo git**: sigue en un solo commit (`48fd58c`). Falta decidir si esta
  sesión (Capa 2) se commitea aparte o junto con lo próximo.
