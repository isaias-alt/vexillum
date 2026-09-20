# 2026-09-20 - Hook `Stop` asíncrono: el commander se entera aunque su turno ya haya terminado

## Resuelto y en pie

- **Hueco real de diseño, encontrado por el general en uso real**: al dispatchar 3 missions en paralelo, el commander terminó su turno (sin nada pendiente en ese momento) y quedó inactivo. Cuando las tres tareas se asentaron minutos después, nadie lo despertó - el hook `Stop` solo puede interceptar un turno que está **en el momento de terminar**, no reactivar una sesión ya inactiva desde afuera. El general lo señaló directamente: "yo no tengo que pedirle al comandante que lo revise" - la razón de ser del sentinel era justamente que no hiciera falta acordarse.

- **Investigado en la referencia real, no inventado**: `firstmate` resuelve exactamente esto. Su `AGENTS.md` sección 8 dice "No turn ends blind while work is under way". Su `.claude/settings.json` real registra el Stop hook con:
  ```json
  { "type": "command", "command": "...fm-claude-stop-autoarm.sh", "asyncRewake": true, "timeout": 28800 }
  ```
  Y su propio script (`bin/fm-claude-stop-autoarm.sh`) documenta el mecanismo: el hook corre en background en cada Stop, puede bloquear en su propio proceso el tiempo que haga falta, y cuando decide que hay algo accionable, **escribe a stderr y sale con código 2** - eso es lo que "wakes Claude even while idle" (entregado como "Stop hook feedback").

- **Verificado en vivo, dos veces, antes de tocar nada de vexillum**: primero con un hook mínimo de prueba (un script que duerme 12s y sale 2) en una sesión de Claude Code real separada - confirmado: "Stop hook feedback" apareció solo en el pane, sin que se mandara ningún prompt nuevo, ~12s después de que el turno ya había terminado. La el mecanismo es real y funciona tal como lo documenta firstmate.

- **Implementado para vexillum** (mucho más simple que firstmate - sin daemons, sin epochs, sin verificación de identidad por PID; vexillum ya tiene un sentinel corriendo independientemente, no hace falta que el hook lo arranque):
  - Nuevo `vexillum sentinel await` (`internal/cli/sentinel.go`): bloquea, resondeando cada 5s (`sentinelAwaitPollInterval`), hasta que aparece una wake o pasan 55 minutos (`sentinelAwaitMaxWait` - deliberadamente por debajo del timeout de 3600s registrado en el hook, para salir limpio por su cuenta antes de que Claude Code lo mate). Si encuentra algo: mensaje a stderr, exit 2 (el mecanismo verificado). Si no: exit 0, silencioso.
  - `vexillum sentinel drain` sigue existiendo sin cambios (chequeo instantáneo, para uso manual/debug) - `await` es un comando nuevo, no un reemplazo.
  - `ensureSentinelHook` reescrito para registrar `await` con `"asyncRewake": true, "timeout": 3600`, y para **migrar en el lugar** un hook viejo (`vexillum sentinel drain`, sin asyncRewake) a la versión nueva en vez de dejarlo duplicado - un proyecto inicializado con una versión anterior de vexillum se actualiza solo la próxima vez que corra `vexillum upgrade`.
  - `productAgentsMD` no necesitó cambios de fondo - la sección "The sentinel" ya decía "you'll be interrupted... the next time you'd otherwise stop", que ahora es literalmente cierto incluso si ya había "terminado" de estar activo.

- **Verificado en vivo por tercera y cuarta vez, con el mecanismo real de vexillum** (no el hook de prueba): un commander real despachando una mission real, primero en un proyecto scratch, después en `vexillum-prueba` mismo (`vexillum upgrade` migró el hook viejo al nuevo sin problema). En los dos casos: el commander despachó, su turno terminó ("done"), y minutos después "Stop hook feedback" apareció solo, sin ningún prompt nuevo - el commander investigó la mission por su cuenta y reportó, tal como se esperaba. En `vexillum-prueba` se completó el ciclo entero (dispatch → wake async → land → release), quedó aterrizado en `07515d5`.

- **9 tests nuevos** (`internal/cli`): `sentinelMode` con "await", `TestRunSentinelAwait_FindsAlreadyPendingWake`, `TestRunSentinelAwait_TimesOutWithNothingPending`, `TestRunSentinelAwait_FindsWakeThatArrivesMidWait`, `TestInit_SentinelStopHookIsAsync`, `TestEnsureSentinelHook_UpgradesLegacySyncHook`. Build, vet, gofmt y toda la suite en verde con `-race`.

## Pendiente para la próxima

- **Múltiples instancias de `vexillum sentinel await` pueden apilarse** en una sesión larga - Claude Code dispara el hook en cada Stop sin deduplicar (confirmado en la doc de firstmate: "Claude does not dedupe async hooks"). Cada instancia es casi gratis (un chequeo de `~/.vexillum/wakes/` cada 5s), y `sentinel.Drain` ya evita que dos instancias reporten la misma wake dos veces (la primera que la lee la marca ack). Se dejó sin lock de single-flight a propósito (simplicidad primero) - si en uso real resulta un problema real de recursos, agregar uno (mismo patrón que `sentinel.AcquireLock`) es sencillo.
- No se investigó el texto sin enviar que apareció repetidamente en los panes de prueba de esta sesión (ej. "push it", "land it", "show me the stack.py file") - parece un artefacto de la UI de herdr o texto que el propio general tipeó en paralelo en otros panes, no algo relacionado con el hook async. Vale la pena tenerlo presente si se repite de forma que interfiera con algo real.
- Repo con cambios sin commitear de esta sesión (comando `await` + migración del hook) - falta que el general revise el mensaje de commit.
