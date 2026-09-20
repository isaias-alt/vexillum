# 2026-09-20 - Rediseño del sentinel: dispatch async, sentinel como única fuente de verdad

## Resuelto y en pie

- **Causa raíz confirmada contra la referencia real (no solo inferida)**:
  se volvió a bajar `bin/fm-spawn.sh` de firstmate con `gh api`. Confirma
  que el spawn real de firstmate nunca bloquea esperando a que el worker
  termine - solo lo lanza y vuelve; la detección de que terminó es 100%
  trabajo del daemon de supervisión (sección 8 de su `AGENTS.md`). El bug
  de carrera que ya se había documentado (`20260920-sentinel-bug-race-y-
  upgrade-pendiente.md`) era exactamente la diferencia entre eso y lo que
  hacía `vexillum dispatch`: bloquear hasta 10 minutos con `agent prompt
  --wait` y escribir el estado final él mismo, dejándole al sentinel una
  tarea que casi nunca alcanzaba a ver como "todavía running".

- **`herdr agent prompt --help` confirma el mecanismo para arreglarlo
  sin inventar nada**: `--wait` es opcional (sin él, dispara y vuelve), y
  con `--timeout` sin `--until` espera el settle completo (eso es lo que
  usaba el código viejo). La solución no fue quitar `--wait` del todo,
  sino acortar el timeout a un sondeo corto (15s, decidido con el
  general vía AskUserQuestion) que sirve para dos cosas: prompts
  triviales se resuelven ahí mismo (rápido, sin depender del sentinel), y
  cualquier cosa real da `timeout` - que ahora se trata como éxito
  parcial ("sigue corriendo"), no como fallo.

- **`internal/soldier/herdr_run.go`**: `RunInHerdr` ahora llama a
  `client.AgentPrompt(agentName, task.Prompt, quickSettleTimeoutMS)` (15s,
  antes 10 minutos). Si `herdr.IsTimeout(err)` (nuevo clasificador en
  `internal/herdr/herdr.go`, código `"timeout"` según la ayuda de herdr -
  **sin verificar en vivo todavía**, ver pendientes), la tarea queda
  `Running` (ya persistida antes del prompt) y la función vuelve sin
  error - el sentinel es la única fuente de verdad para esa transición de
  ahí en más. Cualquier otro error sigue siendo un fallo real
  (`failHerdrTask`), igual que antes.

- **`internal/sentinel/sentinel.go`**: `Tick` ahora captura
  `task.Output` (vía `AgentRead`) en el momento en que detecta una
  transición - antes solo lo capturaba `RunInHerdr` cuando esperaba el
  settle completo él mismo, así que para cualquier tarea real ese output
  se hubiera perdido. También se agregó `IsRunning(vexillumHome) bool`,
  un chequeo de vida del lock sin reclamarlo (a diferencia de
  `AcquireLock`), para que `dispatch` pueda decidir si hace falta
  auto-arrancar uno.

- **`internal/cli/dispatch.go`**: `Dispatch` ahora llama a
  `ensureSentinelRunning` antes de despachar - si no hay un sentinel vivo
  para este `vexillumHome` (`sentinel.IsRunning`), arranca
  `vexillum sentinel` detached (`SysProcAttr{Setsid: true}`, log a
  `~/.vexillum/sentinel.log`) en background. Es best-effort: si falla,
  solo avisa por stderr y sigue - el soldier ya está andando de todas
  formas. **Decisión tomada con el general**: dado que ahora el sentinel
  es imprescindible para cualquier tarea real (sin él, una tarea que
  supera el sondeo de 15s queda `Running` para siempre), no tenía sentido
  seguir dependiendo de que el commander se acuerde de arrancarlo a mano.

- **`productAgentsMD` reescrita** en las secciones "The sentinel" y
  "Dispatching a soldier": ya no le dice al commander que lo arranque a
  mano ni que backgroundee `vexillum dispatch` (antes bloqueaba hasta 10
  minutos; ahora vuelve en segundos). El flujo nuevo: dispatchar inline,
  seguir con otras cosas, y el hook `Stop` interrumpe cuando hay una
  novedad - exactamente el patrón que ya se documentó de firstmate
  ("you don't have to remember to check").

- **Efecto colateral buscado**: esto deja resuelta gran parte de
  "restart-proof" (paso 4 de Capa 4) sin trabajo dedicado - una tarea
  `Running` huérfana por un `vexillum` que murió a mitad de camino es
  ahora el caso normal que el sentinel ya reconcilia en cada tick, no un
  caso especial.

- **Tests nuevos**: `TestRunInHerdr_TimeoutHandsOffToSentinel`,
  `TestRunInHerdr_NonTimeoutPromptErrorFails` (`internal/soldier`),
  `TestTick_CapturesOutputOnTransition`, `TestIsRunning`,
  `TestIsRunning_FalseForStaleLock` (`internal/sentinel`). Build, vet,
  gofmt y toda la suite en verde. `ensureSentinelRunning` (arranque real
  de proceso, escritura de log) queda sin test unitario a propósito -
  mismo criterio que el resto del código de "wrapper delgado" que toca
  el entorno real (os.Getwd, os.UserHomeDir): se verifica en vivo, no
  con un mock del sistema operativo.

## Pendiente para la próxima

- **Verificación en vivo, todavía no hecha**: el código nunca se corrió
  contra un herdr real con una tarea que efectivamente tarde más de 15s.
  Falta confirmar en particular:
  - que el código de error de herdr para un `--wait --timeout` vencido
    sea literalmente `"timeout"` (asumido de la ayuda de `herdr agent
    prompt --help`, no observado en un JSON de error real todavía).
  - que `dispatch` efectivamente vuelva en segundos para una tarea real,
    y que el sentinel auto-arrancado la recoja y dispare el hook `Stop`
    más tarde.
  - que el auto-arranque del sentinel (`Setsid`, log a
    `~/.vexillum/sentinel.log`) sobreviva de verdad a que termine el
    propio proceso `vexillum dispatch`.
  Como siempre, esto se prueba a través del commander en un proyecto
  real (`vexillum-prueba`), no con comandos sueltos.
- Proyectos ya inicializados (como `vexillum-prueba`) van a ver
  `AGENTS.md` reportado como "has local changes" por `vexillum upgrade`
  si no tienen hash guardado todavía (son de antes de esa
  funcionalidad) - hace falta un `vexillum init` extra ahí para que
  quede con hash y `upgrade` pueda refrescarlo solo de ahí en más.
- Repo con cambios sin commitear de esta sesión (rediseño completo) -
  falta que el general revise el mensaje de commit.
