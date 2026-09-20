# 2026-09-20 - CLI real: `vexillum dispatch` / `land` / `release`

## Resuelto y en pie

- **El bug del "Agent tool nativo" volvió a pasar, por la razón de fondo que
  ya habíamos anotado como pendiente**: las instrucciones de despacho
  (`AGENTS.md`) solo vivían en la instancia de prueba de
  `vexillum-prueba`, nunca en la plantilla real
  (`internal/cli/init.go:productAgentsMD`). Al resetear el proyecto (borrar
  `.git`/`.vexillum`) para "arrancar limpio", se perdió todo lo aprendido;
  `vexillum init` volvió a generar el `AGENTS.md` viejo (solo vocabulario),
  y el commander volvió a usar su Agent tool nativo por falta de
  instrucciones - no un bug nuevo, la misma causa de siempre resurgiendo
  porque nunca se cerró el círculo.

- **Se cerró el círculo**: en vez de seguir parchando la instancia de
  prueba a mano cada vez, se construyeron los comandos reales del binario
  `vexillum`, investigando primero cómo lo hace `firstmate` de verdad
  (`AGENTS.md` real del repo, sección 7 "Task lifecycle", y el usage de
  `bin/fm-spawn.sh`) para no inventar la forma:
  - "Spawn only through `bin/fm-spawn.sh`" - confirma que el despacho es
    un subcomando real, no instrucciones sueltas para que el agente arme
    comandos de git/herdr por su cuenta.
  - "local-only has the worker stop with a clean ready branch, then waits
    for the configured merge authority" - confirma el flujo que ya
    habíamos construido con `camp.Land` (aterrizar es una decisión
    aparte, con aprobación).
  - "Tear down a ship task only after landing is confirmed... never an
    obstacle to bypass" - confirma el diseño ya existente de
    `camp.Release`.

- **`internal/cli/dispatch.go`** (nuevo): tres comandos reales,
  `Dispatch`/`Land`/`Release`, con el mismo patrón de testabilidad que
  `init`/`doctor` (función núcleo testeable + wrapper delgado que resuelve
  `os.Getwd()`/`$HOME`/env vars reales). `projectDir` y `vexillumHome` ya
  no se pasan a mano como en el binario de prueba - se resuelven igual que
  `init`/`doctor` (directorio actual, `~/.vexillum`). `Dispatch` acepta
  `--kind mission|scout` (default `mission`).

- **`cmd/vexillum/main.go`** gana los subcomandos `dispatch`, `land`,
  `release` en el switch principal y en el usage.

- **`productAgentsMD` reescrita por completo** con todo lo aprendido esta
  sesión (antes vivía solo en el `AGENTS.md` de `vexillum-prueba`, editado
  a mano una y otra vez): advertencia explícita de no usar el Agent tool
  nativo, cómo despachar (`vexillum dispatch`, en background), reportar
  resultado sin plomería técnica, distinguir mission/scout, aterrizar con
  aprobación por defecto salvo "yolo", liberar el camp. Ahora **todo
  proyecto nuevo que corra `vexillum init` lo tiene de entrada** - no hace
  falta reconstruirlo a mano si el proyecto se resetea.

- **`tmp-demo/` eliminado** - ya cumplió su función (validar el diseño
  contra herdr real antes de comprometerse a la interfaz del CLI real) y
  quedaba obsoleto con los comandos reales.

- **7 tests nuevos** en `internal/cli/dispatch_test.go` (parseo de
  argumentos, rechazo en proyecto no inicializado, dispatch exitoso con un
  `fakeHerdr` mínimo, y land+release de punta a punta con un camp git
  real) - la lógica de seguridad en sí ya está probada a fondo en
  `internal/camp` e `internal/soldier`; estos tests verifican el cableado
  del CLI, no repiten esa cobertura. Build, vet, gofmt y tests verdes (53
  tests en total).

- **Verificado en vivo**: `vexillum --help` muestra los nuevos comandos;
  se reinicializó `vexillum-prueba` desde cero con el binario real y la
  plantilla nueva (`AGENTS.md`/`CLAUDE.md` regenerados con las
  instrucciones completas).

## Confirmado en vivo: el ciclo completo funcionó

- El general corrió la prueba real (mission + scout en paralelo, vía
  `vexillum dispatch` real esta vez): ambos despachados en background,
  scout investigando en internet, mission preguntando antes de aterrizar,
  land rechazado una vez por scaffold sin trackear (esperado), commiteado
  con el ok del general, land rechazado una segunda vez por divergencia
  (esperado - nunca fuerza ni rebasea solo), rebase explícito con
  aprobación, land exitoso, release de ambos camps. Todo el diseño de
  seguridad (nunca forzar, nunca aterrizar sin aprobación, nunca liberar
  sin aterrizar) se ejerció de verdad y se sostuvo.

- **Fricción recurrente identificada**: el scaffold de `vexillum init`
  (`AGENTS.md`, `CLAUDE.md`, `.vexillum/`) nunca se commitea solo, así que
  la primera mission de cualquier proyecto recién inicializado se topa con
  el checkout sucio al intentar aterrizar. Se agregó una nota a
  `productAgentsMD` pidiéndole al commander que, si nota el scaffold sin
  trackear, lo commitee temprano (con el ok del general) en vez de dejar
  que se convierta en una sorpresa a mitad de un `land`. Se evaluó
  gitignorar `.vexillum/` en su lugar, pero se descartó por ahora
  (`.vexillum/config.json` es contenido liviano y real, no vale la pena
  tratarlo como build artifact todavía).

- `vexillum` ahora vive en el PATH real de la máquina de desarrollo, vía
  symlink en `/opt/homebrew/bin/vexillum` apuntando al binario del repo -
  se actualiza solo con cada `go build -o vexillum ./cmd/vexillum`. Antes
  de esto el comando fallaba con exit 127 (`command not found`) porque el
  binario solo existía en la raíz del repo.

## Pendiente para la próxima

- Falta la prueba real de punta a punta del general con la plantilla
  nueva (mission + scout en paralelo, esta vez debería usar `vexillum
  dispatch` de verdad en vez del Agent tool nativo).
- El repo sigue sin commitear toda esta sesión larga (Capa 4 completa:
  paso 1, el bug de `CLAUDE.md`, `camp.Land`, el choque de nombres, la
  reversión de permisos, y ahora el CLI real). Conviene decidir si se
  commitea todo junto o se separa en varios commits por tema antes de que
  crezca más.
- `data/projects.md` / el flag `+yolo` de `firstmate` no tiene ningún
  equivalente persistido en vexillum todavía - la postura "preguntar antes
  de aterrizar" vive solo como texto en el `AGENTS.md`, no hay mecanismo
  para que el general la cambie de forma duradera por proyecto.
