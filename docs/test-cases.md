# Casos de prueba - Vexillum

Documento vivo. La Capa 1 está a nivel de caso concreto (input, acción, resultado esperado): se prueba tal cual. Las Capas 2 a 4 están como criterios de aceptación de alto nivel: qué tiene que ser cierto para dar la capa por terminada. Cuando llegues a construir cada una de esas capas, bajás esos criterios a casos concretos con el mismo formato que la Capa 1.

Convención: cada caso tiene un id (`L1-01`), una precondición, una acción y un resultado esperado. Un caso pasa solo si el resultado esperado se cumple exacto.

---

## CAPA 1 - CLI base (casos concretos)

### `vexillum init`

**L1-01 - init en proyecto limpio**
Precondición: directorio que es un repo git, sin scaffold previo de vexillum, `~/.vexillum/` no existe.
Acción: correr `vexillum init`.
Esperado: se crea `~/.vexillum/`; se crea el scaffold local del proyecto (config local + `AGENTS.md` de producto + `CLAUDE.md` con `@AGENTS.md`); salida confirma qué se creó; exit code 0.

**L1-02 - init es idempotente**
Precondición: un proyecto donde ya se corrió `init` con éxito.
Acción: correr `vexillum init` de nuevo.
Esperado: no se duplica ni se corrompe nada; la salida informa que ya estaba inicializado; no se sobrescribe ningún archivo sin aviso; exit code 0.

**L1-03 - init no sobrescribe cambios del usuario**
Precondición: un proyecto inicializado donde el usuario editó a mano el AGENTS.md de producto.
Acción: correr `vexillum init` de nuevo.
Esperado: el AGENTS.md editado NO se pisa silenciosamente; si init quisiera regenerarlo, avisa y pide confirmación o lo deja intacto; exit code 0.

**L1-04 - init crea `~/.vexillum/` si falta pero el proyecto ya estaba**
Precondición: proyecto con scaffold local presente, pero `~/.vexillum/` borrado a mano.
Acción: correr `vexillum init`.
Esperado: recrea `~/.vexillum/` sin tocar el scaffold local existente; exit code 0.

**L1-04b - init sana un `CLAUDE.md` faltante en un proyecto ya inicializado**
Precondición: proyecto ya inicializado (tiene `.vexillum/config.json` y `AGENTS.md`) pero sin `CLAUDE.md` - por ejemplo, inicializado con una versión de `vexillum` anterior a que este archivo existiera. Sin este shim, Claude Code nunca lee el `AGENTS.md` de producto en absoluto (carga `CLAUDE.md` automáticamente, no un `AGENTS.md` suelto) - encontrado en uso real, no hipotético.
Acción: correr `vexillum init` de nuevo.
Esperado: crea el `CLAUDE.md` faltante sin tocar `AGENTS.md` ni `.vexillum/config.json`; la salida indica que se restauró un archivo faltante; exit code 0.

**L1-05 - init fuera de un repo git**
Precondición: directorio que NO es repo git.
Acción: correr `vexillum init`.
Esperado: mensaje claro de que el directorio no es un repo git; decisión explícita del comportamiento (falla con mensaje, o inicializa pero advierte que las capas de worktrees lo van a requerir). El comportamiento elegido queda documentado y es consistente; el exit code refleja la decisión (0 si inicializa con warning, distinto de 0 si se decide fallar).

**L1-06 - init sin permisos de escritura en HOME**
Precondición: `~/.vexillum/` no se puede crear (permisos).
Acción: correr `vexillum init`.
Esperado: falla con mensaje claro que nombra el problema de permisos y la ruta; no deja estado a medias; exit code distinto de 0.

### `vexillum doctor`

**L1-07 - doctor con entorno completo**
Precondición: Claude Code en PATH, herdr en PATH, tmux en PATH, `~/.vexillum/` existe y es escribible, proyecto inicializado, directorio es repo git.
Acción: correr `vexillum doctor`.
Esperado: una línea por chequeo, todas en estado ok; resumen final "entorno listo"; no modifica nada; exit code 0.

**L1-08 - doctor con Claude Code faltante**
Precondición: Claude Code NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de Claude Code marca faltante; el resto ok; resumen final indica qué falta; exit code distinto de 0.

**L1-09 - doctor con herdr faltante**
Precondición: herdr NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de herdr marca faltante; resumen indica qué falta; exit code distinto de 0.

**L1-10 - doctor con tmux faltante**
Precondición: tmux NO está en PATH; el resto ok.
Acción: correr `vexillum doctor`.
Esperado: la línea de tmux marca faltante (backend de control); el resumen distingue que es el backend de control, no el primario; exit code refleja la política elegida (si tmux es opcional en v1, puede ser warning con exit 0; si se decide obligatorio, exit distinto de 0). La política queda documentada y es consistente.

**L1-11 - doctor en proyecto no inicializado**
Precondición: repo git sin scaffold de vexillum; binarios presentes.
Acción: correr `vexillum doctor`.
Esperado: la línea de "proyecto inicializado" marca faltante y sugiere correr `init`; exit code distinto de 0.

**L1-12 - doctor no modifica nada**
Precondición: cualquier entorno.
Acción: correr `vexillum doctor` y comparar el estado del disco antes y después.
Esperado: ningún archivo creado, modificado ni borrado por doctor; es estrictamente de lectura.

**L1-13 - doctor fuera de un repo git**
Precondición: directorio que no es repo git.
Acción: correr `vexillum doctor`.
Esperado: la línea de git marca que no es repo; resumen lo refleja; exit code según política (coherente con L1-05).

### Transversales Capa 1

**L1-14 - comando desconocido**
Acción: correr `vexillum finish-my-taxes`.
Esperado: mensaje de comando desconocido, referencia a `--help`; exit code distinto de 0.

**L1-15 - help**
Acción: correr `vexillum --help` y `vexillum <cmd> --help`.
Esperado: lista los comandos disponibles (`init`, `doctor`) con descripción; el help por comando explica qué hace; exit code 0.

---

## CAPA 2 - Estado en disco (casos concretos)

Struct `state.Task` (mission o scout), serializado a JSON en `~/.vexillum/tasks/<id>.json`. Sin workers todavía: se crea y se lee a mano (vía tests), invocado por el autor.

**L2-01 - guardar una tarea nueva**
Precondición: no hay tarea previa con ese id.
Acción: crear una `Task` con `state.New` y persistirla con `state.Save`.
Esperado: se crea `~/.vexillum/tasks/<id>.json`; el archivo es JSON válido.

**L2-02 - round-trip fiel (mission y scout)**
Precondición: una tarea creada y guardada.
Acción: `state.Load` sobre el id recién guardado.
Esperado: el struct reconstruido es idéntico al original, campo a campo, tanto para mission como para scout.

**L2-03 - archivo corrupto**
Precondición: un archivo `<id>.json` en `~/.vexillum/tasks/` con contenido que no es JSON válido.
Acción: `state.Load` sobre ese id.
Esperado: error claro, no panic, no datos basura.

**L2-04 - archivo incompleto**
Precondición: un archivo JSON válido pero sin `id` o `kind`.
Acción: `state.Load` sobre ese id.
Esperado: error claro que indica que la tarea está incompleta.

**L2-05 - versión de esquema no soportada**
Precondición: un archivo JSON válido y completo, pero con `schema_version` distinto al soportado.
Acción: `state.Load` sobre ese id.
Esperado: error claro que nombra la versión encontrada y la esperada; no se interpreta como datos válidos.

**L2-06 - escritura atómica**
Precondición: ninguna.
Acción: `state.Save` de una tarea.
Esperado: no queda ningún archivo temporal huérfano en `~/.vexillum/tasks/` tras una escritura exitosa; el único archivo presente es `<id>.json`.

**L2-07 - listar todas las tareas**
Precondición: varias tareas guardadas, mezcla de mission y scout.
Acción: `state.List` sobre `~/.vexillum/`.
Esperado: devuelve todas las tareas guardadas, sin omitir ni duplicar ninguna. Si no hay ninguna tarea guardada (o `tasks/` no existe), devuelve una lista vacía, no error.

**L2-08 - listar con un archivo corrupto**
Precondición: una tarea válida guardada y, además, un archivo corrupto en `~/.vexillum/tasks/`.
Acción: `state.List`.
Esperado: falla con un error claro que nombra el archivo problemático; no ignora la corrupción silenciosamente ni devuelve una lista parcial sin avisar.

## CAPA 3 - Un proceso (casos concretos)

Camp = un slot de un pool de worktrees reutilizables por proyecto (`internal/camp`, inspirado en treehouse). Soldier = el proceso que corre dentro de un camp (`internal/soldier`), con comando inyectable - en esta capa se prueba con comandos de test, no con el binario `claude` real (ver `docs/prd-v1.md`, Capa 3). Sin CLI todavía: se ejercita a mano, vía tests.

**L3-01 - crear un camp aislado**
Precondición: un proyecto git limpio, sin camps previos.
Acción: `camp.Acquire` para una tarea nueva.
Esperado: se crea un worktree en su propia rama (`vexillum/<task-id>`); el working tree del proyecto (rama actual, archivos) no se toca.

**L3-02 - lanzar el soldier y capturar salida + exit code (éxito)**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que termina con exit 0.
Esperado: el `Task` devuelto y el persistido en disco quedan en `status=done`, con la salida capturada y `exit_code=0`; se registra el camp asignado (`camp_slot`, `camp_path`, `camp_branch`).

**L3-03 - detectar y reflejar un fallo del proceso**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que termina con exit distinto de 0.
Esperado: `status=failed`, `exit_code` refleja el código real; `Run` no devuelve error de Go (es un resultado esperado del soldier, no una falla de vexillum); el camp queda con dueño claro en el pool (no huérfano), aunque no se libera automáticamente.

**L3-04 - escritura "running" antes de arrancar**
Precondición: un camp adquirido.
Acción: `soldier.Run` con un comando que tarda un momento en terminar; se lee el estado desde otro punto mientras el proceso sigue corriendo.
Esperado: el estado leído en pleno vuelo muestra `status=running`; si vexillum muriera en ese instante, el estado en disco no miente (queda "running", no "pending" ni un "done" falso).

**L3-05 - Release nunca destruye un camp sucio**
Precondición: un camp adquirido con cambios sin commitear.
Acción: `camp.Release`.
Esperado: falla con un error claro; el camp no se toca.

**L3-06 - Release nunca devuelve al pool un camp no aterrizado**
Precondición: un camp adquirido con commits que todavía no están mergeados en la rama base del proyecto.
Acción: `camp.Release`.
Esperado: falla con un error claro; el slot sigue leased.

**L3-07 - reuso del pool**
Precondición: un camp liberado (limpio y aterrizado).
Acción: `camp.Acquire` para una tarea nueva.
Esperado: reutiliza el mismo slot y el mismo directorio de worktree (no crea uno nuevo); dependencias/build cache del worktree quedan intactos porque nunca se borró.

**L3-08 - un slot leased nunca se reutiliza**
Precondición: un camp adquirido y todavía sin liberar.
Acción: `camp.Acquire` para una segunda tarea, sobre el mismo proyecto.
Esperado: se crea un slot distinto; el slot en uso no se toca.

**L3-09 - verificación de dueño del slot**
Precondición: un camp adquirido por la tarea A.
Acción: `camp.Release` invocado con el id de una tarea B distinta.
Esperado: falla con un error claro que nombra al dueño real; no libera el slot.

**L3-10 - aterrizar (Land) un camp limpio**
Precondición: un camp adquirido, con cambios commiteados, sin divergencia respecto a la rama base del proyecto.
Acción: `camp.Land`.
Esperado: el checkout principal del proyecto queda fast-forwardeado a la rama del camp (mismo HEAD); no crea merge commit, no fuerza nada.

**L3-11 - Land se niega ante una rama divergida**
Precondición: un camp adquirido y commiteado, pero la rama base del proyecto avanzó con commits propios después de que se creó el camp (ya no es fast-forward).
Acción: `camp.Land`.
Esperado: falla con un error claro que dice que hay divergencia y sugiere rebasear; el checkout del proyecto queda intacto (no mergea, no fuerza, no rebasea por su cuenta).

**L3-12 - Land se niega a tocar un checkout sucio**
Precondición: el checkout principal del proyecto (no el camp) tiene cambios sin commitear.
Acción: `camp.Land`.
Esperado: falla con un error claro; no toca el checkout del proyecto.

## CAPA 4 - Concurrencia

Capa grande, partida en pasos verificables (ver `docs/prd-v1.md`, sección Capa 4). Paso 1 - soldier real en un pane de herdr, todavía secuencial - tiene casos concretos abajo. Los pasos 2 a 5 (N en paralelo, sentinel, restart-proof, aislamiento de fallos) quedan como criterios de alto nivel hasta que se construyan.

### Paso 1 - soldier en un pane real de herdr (casos concretos)

`internal/herdr` (wrapper del CLI de herdr) + `soldier.RunInHerdr` (`internal/soldier`). Reemplaza el `os/exec` headless de la Capa 3 por una sesión interactiva real, corriendo con `--dangerously-skip-permissions` (decisión revisada: ver `docs/prd-v1.md`, sección Capa 4) - el control de seguridad real es la aprobación humana en `camp.Land` antes de aterrizar, no un bloqueo por permiso a mitad de tarea. `blocked` sigue siendo un estado posible (una pregunta genuina del agente), solo que ahora es la excepción. Sin CLI todavía, sin paralelismo todavía - un soldier a la vez, invocado a mano.

**L4-01 - correr un soldier de punta a punta**
Precondición: un camp adquirido, un workspace de herdr válido.
Acción: `soldier.RunInHerdr`.
Esperado: crea un tab+pane en el workspace dado (vía `herdr tab create`), arranca el agente, manda el prompt, y persiste el `Task` con `status=done`, los ids de herdr (`herdr_workspace_id`, `herdr_tab_id`, `herdr_pane_id`, `herdr_agent_name`) y el transcript capturado.

**L4-02 - escritura "running" antes de arrancar el agente**
Precondición: un camp adquirido.
Acción: `soldier.RunInHerdr`, inspeccionando el estado justo antes de que se llame `agent start`.
Esperado: el `Task` ya está persistido en `status=running` con el camp y los ids de herdr asignados, antes de que el agente arranque.

**L4-03 - reflejar un bloqueo**
Precondición: un camp adquirido; el agente termina su turno en estado `blocked` (necesita aprobación o input).
Acción: `soldier.RunInHerdr`.
Esperado: `Task.Status = blocked`, sin error de Go (es un resultado esperado, el sentinel lo va a escalar más adelante).

**L4-04 - diálogo de confianza en un camp nuevo**
Precondición: el agente queda `agent_not_ready` al arrancar, con el diálogo de "¿confiás en esta carpeta?" en pantalla.
Acción: `soldier.RunInHerdr`.
Esperado: reconoce el diálogo específico, lo descarta (`down`, `enter`), y sigue normalmente. Un bloqueo de arranque no reconocido (cualquier otro diálogo) **no** se adivina: falla con error claro, sin mandar teclas a ciegas.

**L4-05 - fallo al crear el pane**
Precondición: `herdr tab create` falla (workspace inválido, por ejemplo).
Acción: `soldier.RunInHerdr`.
Esperado: error de Go real; no se persiste ningún estado a medias (nunca se llegó a guardar `running`).

**L4-06 - cerrar el pane al liberar un camp aterrizado**
Precondición: un camp adquirido, con cambios commiteados y ya mergeados en la rama base (aterrizado).
Acción: `soldier.ReleaseInHerdr`.
Esperado: libera el slot del pool (`camp.Release`) **y**, recién ahí, cierra el tab de herdr - mismo momento, no antes.

**L4-07 - el pane queda abierto si el camp no se puede liberar**
Precondición: un camp adquirido con cambios sin commitear (dirty).
Acción: `soldier.ReleaseInHerdr`.
Esperado: falla (mismo motivo que `camp.Release` solo); el tab de herdr no se cierra - sigue habiendo algo para inspeccionar.

**L4-08 - choque de nombre de agente**
Precondición: dos tareas cuyos prompts generan el mismo slug (`vx-<slug>`), lanzadas en paralelo; la primera ya tiene ese nombre en uso en herdr.
Acción: `soldier.RunInHerdr` para la segunda tarea.
Esperado: `agent start` con el nombre candidato falla con `agent_name_taken`; se reintenta una vez con un nombre desambiguado (`vx-<slug>-<sufijo del id>`); ese es el nombre que se usa para el resto de la interacción y el que queda persistido en `Task.HerdrAgentName` - no falla toda la corrida.

### CLI real: `vexillum dispatch` / `land` / `release`

Reemplaza el binario descartable `tmp-demo/soldier-demo` (borrado). Mismo mecanismo que paso 1, ahora como subcomandos reales del binario (`internal/cli/dispatch.go`), con `projectDir`/`vexillumHome` resueltos igual que `init`/`doctor` (directorio actual / `~/.vexillum`), no pasados a mano.

**L4-09 - dispatch se niega en un proyecto no inicializado**
Precondición: repo git sin scaffold de vexillum (`.vexillum/config.json` ausente).
Acción: `vexillum dispatch "<prompt>"`.
Esperado: falla con mensaje claro que sugiere `vexillum init`; no crea ningún camp.

**L4-10 - dispatch de punta a punta**
Precondición: proyecto inicializado.
Acción: `vexillum dispatch "<prompt>" [--kind mission|scout]` (default `mission`).
Esperado: crea la tarea, adquiere el camp, corre `soldier.RunInHerdr`, imprime `task_id`, `status`, ids de herdr, y el transcript; exit code refleja si `RunInHerdr` tuvo un error real (no un `blocked`/`done` normal).

**L4-11 - land y release reales**
Precondición: una tarea despachada con `dispatch`, con commits landeables.
Acción: `vexillum land <task-id>` y después `vexillum release <task-id>`.
Esperado: ambos resuelven el camp desde `task.CampSlot` (vía `camp.Resolve`) y delegan en `camp.Land` / `soldier.ReleaseInHerdr` - mismas garantías de seguridad ya probadas en esos paquetes.

## Paso 2 - sentinel (casos concretos)

`internal/sentinel`: polling (no `events.subscribe` - mismo fallback que `firstmate` documenta como "permanente", ver `docs/prd-v1.md`), no daemon con push real. `vexillum sentinel` corre el loop en foreground; `vexillum sentinel drain` es lo que llama el hook `Stop` de Claude Code (agregado por `vexillum init` a `.claude/settings.json`).

**L4-12 - detectar una transición y registrar una wake**
Precondición: una tarea con `status=running` y `herdr_agent_name` asignado; el `agent_status` real en herdr cambió (ej. a `done`).
Acción: `sentinel.Tick`.
Esperado: persiste el nuevo status en el `Task`; registra una `Wake` sin ack en `~/.vexillum/wakes/<id>.json`.

**L4-13 - sin transición, sin wake**
Precondición: una tarea `running` cuyo `agent_status` real sigue siendo `working` (todavía no asentó).
Acción: `sentinel.Tick`.
Esperado: no registra ninguna wake, no toca el `Task`. (`working` mapea a `StatusRunning`, no a fallo - bug encontrado y corregido en esta misma sesión.)

**L4-14 - una wake se entrega una sola vez**
Precondición: una wake sin ack ya registrada.
Acción: `sentinel.Drain` dos veces seguidas.
Esperado: la primera devuelve la wake y la marca ack; la segunda no devuelve nada.

**L4-15 - drain sin wakes pendientes**
Precondición: ninguna wake sin ack.
Acción: `vexillum sentinel drain`.
Esperado: imprime `{}` (formato exacto que un hook `Stop` interpreta como "dejar terminar el turno normalmente").

**L4-16 - drain con wakes pendientes bloquea el `Stop`**
Precondición: al menos una wake sin ack.
Acción: `vexillum sentinel drain`.
Esperado: imprime `{"decision":"block","reason":"..."}` nombrando la tarea y la transición - el hook `Stop` de Claude Code impide que el turno termine e inyecta ese motivo como contexto.

**L4-17 - lock de instancia única**
Precondición: un `vexillum sentinel` ya corriendo (o un lock huérfano de un proceso muerto).
Acción: `sentinel.AcquireLock` de nuevo.
Esperado: se niega si el proceso dueño del lock sigue vivo (nombra el pid); si el pid ya no existe, reclama el lock sin problema (no queda bloqueado para siempre por un proceso muerto).

**L4-18 - `vexillum init` agrega el hook `Stop`**
Precondición: proyecto sin `.claude/settings.json`, o con uno existente con otros hooks/settings.
Acción: `vexillum init` (o `ensureSentinelHook` directamente).
Esperado: agrega el hook de `vexillum sentinel drain` sin pisar nada existente; correrlo de nuevo no duplica el hook; un `settings.json` con JSON inválido se deja intacto y se reporta como error, nunca se sobrescribe a ciegas.

**L4-19 - `dispatch` no se autoreporta para trabajo real**
Precondición: un soldier real que tarda más que el sondeo rápido (`quickSettleTimeoutMS`, 15s).
Acción: `vexillum dispatch`.
Esperado: vuelve tras el sondeo corto con `status=running`, no bloquea hasta que el soldier termina; el sentinel (auto-arrancado si no había uno corriendo) es quien detecta y persiste la transición final más tarde. Verificado en vivo (ver binnacle `20260920-sentinel-rediseno-async.md` y `20260920-bugs-primera-prueba-en-vivo.md`).

**L4-20 - el sentinel no confunde "recién arrancado" con "terminado"**
Precondición: un soldier recién despachado, con el sentinel corriendo en paralelo.
Acción: `sentinel.Tick` sondea el estado en vivo casi al mismo tiempo que `dispatch` somete el prompt.
Esperado: ignora cualquier tarea `running` cuyo `UpdatedAt` sea más reciente que `settleGracePeriod` (8s) - un `idle` leído en esa ventana (el agente todavía no arrancó a procesar el prompt) nunca se confunde con un asentamiento real. Bug real encontrado y arreglado en vivo (ver binnacle `20260920-bugs-primera-prueba-en-vivo.md`).

**L4-21 - despachar desde adentro de un camp se rechaza**
Precondición: el directorio de trabajo actual es un camp (`projectDir` está bajo `vexillumHome`).
Acción: `vexillum dispatch`/`init`/`upgrade`/`land`/`release`/`sentinel`.
Esperado: falla con un mensaje claro en vez de crear un pool de camps fantasma bajo el hash de la ruta del camp. Bug real encontrado y arreglado en vivo.

**L4-22 - un agente genuinamente desaparecido marca la tarea `interrupted`, no la deja `running` para siempre**
Precondición: una tarea `running` cuyo agente de herdr fue destruido de verdad (pane cerrado a mano, herdr reiniciado) - `AgentStatus` devuelve `agent_not_found`, no un error transitorio.
Acción: `sentinel.Tick`, repetido a lo largo de `notFoundConfirmWindow` (10s).
Esperado: la primera observación de `agent_not_found` solo registra el momento (`AgentNotFoundSince`), sin tocar el estado - evita condenar a una tarea por un hipo pasajero de herdr. Si sigue sin encontrarse pasado el período de confirmación, la tarea pasa a `state.StatusInterrupted`, se registra una wake, y se anota en `Output` que el pane desapareció. Si el agente se resuelve bien en el medio (era un hipo, no una desaparición real), la marca se limpia. Verificado en vivo: se cerró el pane de una mission real a mano, y quedó `interrupted` exactamente 10s después de la primera observación (ver binnacle `20260920-restart-proof-agente-desaparecido.md`).

**L4-23 - un `agent prompt` "atascado" se reintenta, no se trata como fallo**
Precondición: `agent prompt --wait` devuelve `agent_prompt_stalled` (herdr no observó `working`/`blocked` en su propia ventana interna de ~5s) - observado en vivo justo después de arrancar un agente recién creado, con el prompt nunca inyectado en el pane (confirmado leyendo la transcripción, vacía).
Acción: `soldier.RunInHerdr`.
Esperado: reintenta el envío del prompt hasta `promptStalledMaxAttempts` (3) antes de fallar - un reintento simple lo resolvió al instante en la prueba en vivo. Solo falla la tarea si sigue atascado después de agotar los reintentos.

**L4-24 - el nombre de fallback por colisión nunca excede el límite de herdr**
Precondición: el nombre candidato del agente ya está en el límite de 32 caracteres de herdr, y colisiona con un agente vivo (`agent_name_taken`).
Acción: `soldier.RunInHerdr` (vía `disambiguatedName`).
Esperado: el nombre de fallback (candidato + sufijo) se trunca para nunca superar 32 caracteres, en vez de que herdr lo rechace con `invalid_agent_name` - encontrado en vivo con el prompt "Write a Python module implementing a simple binary search tree..." (candidato de 32 caracteres exactos).

**L4-25 - una lectura de estado fallida no tumba el resto del tick**
Precondición: una tarea `running` cuyo `AgentStatus` falla con un error transitorio genérico (no `agent_not_found`, no un error estructurado de herdr).
Acción: `sentinel.Tick` sobre una lista con esa tarea (y, en el caso real, otras tareas junto a ella).
Esperado: la tarea con la lectura fallida se saltea (sin wake, sin cambio de estado) - `Tick` no devuelve error ni aborta el resto del loop; cualquier otra tarea en la misma corrida se procesa igual.

**L4-26 - el pane se cierra mientras se somete el prompt**
Precondición: `AgentPrompt` devuelve `agent_not_running` - confirmado en el changelog de herdr (`Cellar/herdr/0.9.0/CHANGELOG.md`): código que devuelve una llamada `--wait` específicamente cuando el pane objetivo se cierra mientras se espera. Encontrado en vivo en el primer intento de una prueba real de commander, antes de que nadie tocara nada.
Acción: `soldier.RunInHerdr`.
Esperado: falla la tarea (no tiene sentido reintentar - el pane está genuinamente cerrado, a diferencia de `agent_prompt_stalled`) con un mensaje explícito y accionable ("... safe to redispatch") en vez del string crudo de herdr. `sentinel.Tick` no necesita el mismo tratamiento: usa `AgentStatus` (sin `--wait`), que para un pane realmente cerrado siempre da `agent_not_found` (verificado matando el proceso subyacente a mano), nunca `agent_not_running` - los dos códigos están escopeados a formas de llamada distintas.

**L4-27 - `camp.Acquire` concurrente nunca colisiona un slot**
Precondición: N tareas dispatchadas al mismo tiempo (N procesos `vexillum dispatch` reales, o N llamadas concurrentes a `camp.Acquire` sobre el mismo pool).
Acción: N `camp.Acquire` en simultáneo.
Esperado: cada una obtiene un slot y un worktree genuinamente distintos; `pool.json` termina con exactamente N slots, cada uno arrendado por una sola tarea. Bug real encontrado: sin lock, `Acquire` hace un read-modify-write sin exclusión mutua sobre `pool.json` (leer, calcular el próximo slot libre, escribir) - dos llamadas concurrentes pueden leer el mismo estado antes de que ninguna escriba, calcular el mismo número de slot, y correr `git worktree add` en la misma ruta. Reproducido 100% de las veces con 8 llamadas concurrentes antes del fix (`internal/camp/camp_test.go`); arreglado con un `flock` exclusivo (`lockPool`) alrededor de la sección crítica. Verificado también con `vexillum dispatch` real: 3 procesos genuinamente concurrentes, cada uno con su slot/camp/rama/commit propios, sin colisión.

**L4-28 - `camp.Release` concurrente no pierde actualizaciones**
Precondición: N camps arrendados, liberados todos al mismo tiempo.
Acción: N `camp.Release` en simultáneo.
Esperado: los N terminan sin error, y `pool.json` refleja los N slots correctamente liberados - ninguna escritura se pisa con otra por la misma falta de exclusión mutua que L4-27. Mismo `lockPool` cubre este caso.

## Pendiente (paso 5, criterio de alto nivel; pasos 3 y 4 mayormente cubiertos)

- N soldiers corren en paralelo, cada uno en su propio camp, sin pisarse entre ellos ni corromper estado compartido - ✅ formalizado (L4-27, L4-28) y verificado en vivo con `vexillum dispatch` real concurrente. Sigue pendiente un caso con múltiples soldiers reales corriendo tiempo real en simultáneo (no solo `camp.Acquire`/`Release` aislados) para cerrar del todo el paso 5 (aislamiento de fallos) con uno de ellos fallando a propósito.
- El sentinel detecta, vía la socket API de herdr, qué soldier está bloqueado o terminó, sin sondeo activo que gaste tokens (push vía `events.subscribe`, con fallback a polling) - decisión consciente de quedarse solo con el fallback de polling (ver binnacle `20260920-sentinel-paso2.md`), no pendiente.
- Restart-proof: ✅ cubierto en la parte de reconciliación (L4-19, L4-20, L4-22) - una tarea que quedó `running` huérfana (ya sea porque `vexillum` murió a mitad de dispatch, o porque el agente mismo desapareció) siempre se resuelve, nunca queda colgada para siempre. **Todavía sin implementar**: la parte de "los que se puedan resumir se resumen" - hoy una tarea `interrupted` solo se detecta y reporta, no hay ningún mecanismo para relanzar un agente en el mismo camp/rama continuando el trabajo.
- Una mission termina entregando cambios de código (un PR); un scout termina dejando un reporte de investigación; ambos resultados quedan persistidos y asociados a su tarea. - cubierto por Capa 3/4 paso 1.
- Un soldier que falla no tumba a los demás ni al commander; su fallo queda aislado y reflejado en su estado - cubierto estructuralmente (ver L4-25), falta un caso formal con múltiples soldiers reales en paralelo, uno de ellos fallando.
- Dos soldiers nunca comparten el mismo camp ni la misma rama - cubierto por el pool de camps de Capa 3 (cada `camp.Acquire` asigna un slot propio); L4-21 cierra el caso donde un comando corrido desde el lugar equivocado podía romper esta garantía.
