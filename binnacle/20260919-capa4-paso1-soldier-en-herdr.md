# 2026-09-19 - Capa 4, paso 1: soldier real en un pane de herdr

## Resuelto y en pie

- **Capa 4 partida en pasos verificables**, no construida de una vez: 1)
  soldier en pane de herdr (secuencial) - esta sesión; 2) sentinel
  event-driven; 3) N en paralelo; 4) restart-proof; 5) aislamiento de
  fallos. Documentado en `docs/prd-v1.md` y `docs/test-cases.md`.

- **herdr no reemplaza a `camp`**: se verificó contra el repo real de
  `firstmate` (`docs/herdr-backend.md`, no de memoria) que ahí tampoco lo
  hace - "Herdr provides the terminal session while Treehouse continues to
  provide task worktrees". `internal/camp` no se tocó en esta sesión, sigue
  siendo git puro. herdr solo se usa para el tab/pane donde corre el
  soldier, apuntado al `camp.Path` que `camp` ya creó.

- **Sin workspace dedicado para vexillum**: descartado a pedido del general
  y confirmado por el propio skill file de herdr (`herdr --skill`): "Do not
  create a workspace... unless the user explicitly requests that
  topology." Los soldiers son tabs nuevos dentro del workspace desde el que
  se despacha (`$HERDR_WORKSPACE_ID`, heredado por herdr en todo pane que
  maneja). Si esa variable no está seteada, `RunInHerdr` recibe el
  workspace como parámetro explícito (no lee el env var ella misma, por
  testabilidad) y el llamador es responsable de resolverlo y fallar si
  falta.

- **Decisión de permisos reabierta y cambiada**: en Capa 3 se había elegido
  correr con `--dangerously-skip-permissions` porque el soldier headless no
  tenía forma de que algo reaccionara a un bloqueo. Con herdr real, un
  agente que queda esperando aprobación se detecta como `agent_status:
  blocked` (validado en vivo con `herdr agent prompt --wait`). El general
  decidió pasar a permisos normales: el soldier puede quedar `blocked`, y
  el sentinel (paso 2, todavía no construido) es quien va a escalar eso al
  commander. `soldier.RunInHerdr` ya no usa `ClaudeCommand`/
  `--dangerously-skip-permissions` en absoluto - arranca el agente vía
  `herdr agent start --kind claude` (sesión interactiva real).

- **`internal/herdr`**: wrapper del CLI de `herdr` (no socket crudo). El
  propio skill file dice que el binario instalado es la autoridad de
  sintaxis y que los comandos de control devuelven JSON; shell-earlo es más
  simple y robusto que reimplementar el protocolo de socket a mano.
  `Client` es una interfaz (no un struct concreto) específicamente para que
  `internal/soldier` se pueda testear sin un herdr real ni una sesión de
  Claude Code real - mismo criterio de testabilidad que ya se usó con
  `CommandSpec` en la Capa 3.

- **`soldier.RunInHerdr`** (`internal/soldier/herdr_run.go`): crea el
  tab+pane (`herdr tab create`), persiste el `Task` en `running` con los
  ids de herdr *antes* de arrancar el agente (mismo criterio write-ahead de
  Capa 3), arranca el agente (`agent start`), manda el prompt y espera
  (`agent prompt --wait`), captura el transcript (`agent read`), y mapea el
  `agent_status` final a `Task.Status`: `blocked` → `StatusBlocked`
  (nuevo), `idle`/`done` → `StatusDone`, cualquier otra cosa → `StatusFailed`.
  Ya no hay exit code (es una sesión interactiva, no un comando que
  termina) - el campo `ExitCode` queda como algo que solo aplica al `Run`
  headless de Capa 3, que se mantuvo sin tocar.

- **Diálogo de confianza de Claude Code, manejado sin adivinar**: un camp
  es siempre un directorio nunca visto, y la primera vez que Claude Code
  corre interactivo ahí muestra el diálogo "¿confiás en esta carpeta?" -
  herdr lo reporta como `agent_not_ready` en vez de "listo". Se reconoce
  específicamente ese texto (`agent read` + buscar "trust this folder") y
  se descarta con `agent send-keys down enter`, validado en vivo primero a
  mano (comandos sueltos de `herdr`) y después con el código real. Un
  bloqueo de arranque que no sea ese diálogo puntual **no** se adivina: el
  código falla con un error claro en vez de mandar teclas a ciegas a una UI
  que no reconoce.

- **Hallazgo empírico, no documentado por Anthropic**: los worktrees de un
  mismo repo ya confiado parecen heredar esa confianza (no se repite el
  diálogo por cada path nuevo de un camp) - se observó corriendo el flujo
  real contra un camp nuevo de `vexillum-prueba` y el diálogo no apareció.
  Buena noticia (menos fricción en la práctica), pero no es algo que
  vexillum controle ni pueda asumir con certeza para siempre - la lógica de
  detección queda como red de seguridad.

- **`state.Task` sube a schema v3**: se agregan `HerdrWorkspaceID`,
  `HerdrTabID`, `HerdrPaneID`, `HerdrAgentName`, y el status `blocked`.

- **6 tests nuevos** en `internal/soldier/herdr_run_test.go`, con un
  `herdr.Client` falso (`fakeHerdr`) que no toca ningún proceso real:
  corrida exitosa, write-ahead de "running", reflejar `blocked`, descartar
  el diálogo de confianza, no adivinar un bloqueo no reconocido, y fallo al
  crear el tab sin dejar estado a medias. `L4-01` a `L4-05` bajados en
  `docs/test-cases.md`.

- **Verificación E2E doble**: primero a mano, comando por comando del CLI
  de `herdr` contra una sesión real (crear tab, toparse con el diálogo de
  confianza de verdad, descartarlo, mandar un prompt real, leer el
  resultado) - antes de escribir una sola línea de Go. Después con el
  código real (`tmp-demo/soldier-demo` actualizado), contra un camp
  recién creado de `vexillum-prueba`, workspace real (`wV`, la propia
  sesión de este trabajo). Ambas corridas funcionaron de punta a punta;
  los tabs de prueba se cerraron al terminar.

- **Build, vet, gofmt y tests verdes**: 39 tests en todo el repo.

## Pendiente para la próxima

- **Paso 2 (sentinel event-driven)**: `events.subscribe` a
  `pane.agent_status_changed`, con fallback a polling cuando el push no
  esté disponible (criterio validado en `firstmate`,
  `docs/herdr-backend.md`, sección "Push events and polling fallback").
  Sin esto, cada soldier bloqueado/terminado hoy solo se detecta corriendo
  `RunInHerdr` de punta a punta (bloqueante) - no hay forma de enterarse
  de un cambio de estado sin estar esperando activamente.
- **Paso 3 (N en paralelo)**: varios `camp.Acquire` + `RunInHerdr`
  simultáneos, verificar que no se pisan (ids de herdr distintos, slots
  distintos - ya cubierto en parte por Capa 3, pero falta probarlo con
  paralelismo real).
- **Paso 4 (restart-proof)**: reconciliar `state.Task` contra `pane.list`
  real de herdr al reiniciar.
- **Repo git**: esta sesión sigue sin commitear.
